// BFFHandler is the browser-facing auth edge. Its methods are split by
// responsibility across this package:
//
//   - bff_handler.go: construction, readiness, session cookie and response
//     helpers shared by every handler
//   - proxy.go: policy-checked reverse proxy to upstream services
//   - oauth_handler.go: Hydra authorization-code flow, consent, callback, logout
//   - login_handler.go: Kratos login acceptance into Hydra, including login MFA
//   - kratos_handler.go: Kratos self-service flow proxies
//   - session_resolve.go: IAM user context resolution and caching
//   - session_handler.go: /me, session renewal, activity, session list
//   - mfa_handler.go: step-up and recent-auth
//   - tenant_handler.go: active tenant switch
//   - device.go: device tracking and device cookie
package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/policy"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
	"github.com/arda-labs/arda/libs/go/arda-auth/permission"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

const (
	deviceCookieName      = "arda_did"
	deviceCookieMaxAge    = 365 * 24 * 60 * 60
	rememberMFACookieName = "arda_rmf"
	oauthStateMaxAge      = 10 * 60
	loginRememberMaxAge   = 30 * 24 * 60 * 60
)

type BFFHandler struct {
	cfg              config.Config
	store            session.Store
	iamClient        iamclient.Client
	policy           *policy.Policy
	logger           *slog.Logger
	cache            *userContextCache
	resolveMu        sync.Mutex
	resolveInflight  map[string]*sessionUserResolveCall
	httpClient       *http.Client
	streamHTTPClient *http.Client
	// sessionTouchMu guards sessionTouches, the throttle that keeps
	// IAM last_seen_at fresh without a write per request.
	sessionTouchMu sync.Mutex
	sessionTouches map[string]time.Time
}

// sessionTouchInterval bounds how often one session is reported to IAM as
// active; the admin session list needs minutes, not per-request precision.
const sessionTouchInterval = 5 * time.Minute

func NewBFFHandler(cfg config.Config, store session.Store, iamClient iamclient.Client, pol *policy.Policy) *BFFHandler {
	return &BFFHandler{
		cfg: cfg, store: store, iamClient: iamClient, policy: pol, logger: slog.Default(),
		cache:            newUserContextCache(time.Duration(cfg.IAMContextCacheTTL) * time.Second),
		resolveInflight:  make(map[string]*sessionUserResolveCall),
		httpClient:       &http.Client{Timeout: 30 * time.Second, CheckRedirect: passUpstreamRedirect},
		streamHTTPClient: &http.Client{CheckRedirect: passUpstreamRedirect},
	}
}

func (h *BFFHandler) slowLogSettings() (bool, time.Duration) {
	threshold := time.Duration(h.cfg.SlowRequestLogThresholdMS) * time.Millisecond
	return h.cfg.SlowRequestLogEnabled && threshold > 0, threshold
}

func (h *BFFHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if h.iamClient == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "iam": "client_not_configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.iamClient.Ready(ctx); err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "iam": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *BFFHandler) doPut(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return h.httpClient.Do(req)
}

func (h *BFFHandler) setSessionCookie(w http.ResponseWriter, id string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: h.cfg.SessionCookieName, Value: id, Path: "/", HttpOnly: true, Secure: h.cfg.CookieSecure, SameSite: parseSameSite(h.cfg.CookieSameSite), MaxAge: int(ttl.Seconds())})
}

func (h *BFFHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: parseSameSite(h.cfg.CookieSameSite),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (h *BFFHandler) readSessionCookie(r *http.Request) string {
	c, err := r.Cookie(h.cfg.SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip, _, ok := strings.Cut(fwd, ","); ok {
			return normalizeIP(strings.TrimSpace(ip))
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(r.RemoteAddr)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func looksLikeUUID(value string) bool {
	return len(value) == 36 && strings.Count(value, "-") == 4
}

func normalizeIP(value string) string {
	return strings.Trim(value, "[]")
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	ardahttp.SetRequestCorrelation(w, responseRequest(w))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondRequestError(w, responseRequest(w), status, msg)
}

// responseRequest preserves the request correlation initialized by the HTTP
// middleware for helpers whose historical call sites only received a writer.
// New handlers should pass the original request directly.
func responseRequest(w http.ResponseWriter) *http.Request {
	r := &http.Request{Header: make(http.Header)}
	if requestID := strings.TrimSpace(w.Header().Get(ardahttp.HeaderRequestID)); requestID != "" {
		r.Header.Set(ardahttp.HeaderRequestID, requestID)
	}
	if traceParent := strings.TrimSpace(w.Header().Get(ardahttp.HeaderTraceParent)); traceParent != "" {
		r.Header.Set(ardahttp.HeaderTraceParent, traceParent)
	}
	return r
}

func respondRequestError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	code := ardaerrors.CodeForStatus(status)
	if isMachineErrorCode(msg) {
		code = msg
	}
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, msg))
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}

// requirePermission enforces the same session + permission contract as
// policy.yaml for handlers that are not proxied through Proxy: a valid BFF
// session, a resolvable user context, and either the global admin capability
// or one of codes. It writes the 401/403 problem response on denial.
func (h *BFFHandler) requirePermission(w http.ResponseWriter, r *http.Request, codes ...string) bool {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondRequestError(w, r, http.StatusUnauthorized, "not_authenticated")
		return false
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil {
		h.clearSessionCookie(w)
		respondRequestError(w, r, http.StatusUnauthorized, "not_authenticated")
		return false
	}
	if !h.ensureSessionUser(r.Context(), sess, false) {
		h.clearSessionCookie(w)
		respondRequestError(w, r, http.StatusUnauthorized, "user_context_unavailable")
		return false
	}
	if sess.User != nil && (sess.User.IsGlobalAdmin || permission.HasAny(sess.User.Permissions, codes...)) {
		return true
	}
	respondRequestError(w, r, http.StatusForbidden, "insufficient_permissions")
	return false
}

// PolicyRoutes exposes the authorization policy entries read-only (W6a
// resource-management screen): id, path, methods, auth, risk, permissions.
// The route is registered directly (not proxied), so it repeats the
// admin-read policy contract before exposing the authorization surface.
func (h *BFFHandler) PolicyRoutes(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, "iam.user.read") {
		return
	}
	routes := []any{}
	if h.policy != nil {
		for _, route := range h.policy.Routes {
			routes = append(routes, map[string]any{
				"id":          route.ID,
				"path":        route.Path,
				"methods":     route.Methods,
				"auth":        route.Auth,
				"risk":        route.Risk,
				"permissions": route.Permissions,
			})
		}
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, routes)
}

func isMachineErrorCode(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t") {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && char != '.' && char != '_' {
			return false
		}
	}
	return strings.ContainsAny(value, "._")
}
