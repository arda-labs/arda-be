package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
	"github.com/arda-labs/arda/libs/go/arda-auth/permission"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// passUpstreamRedirect keeps upstream redirects visible to the browser. Media
// retrieval answers with a 302 to a short-lived presigned URL; following it
// here would force every object byte through the gateway and cap the transfer
// at the client timeout. Let the browser fetch storage directly.
func passUpstreamRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func (h *BFFHandler) Proxy(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	slowLogEnabled, slowLogThreshold := h.slowLogSettings()
	requestID := ardahttp.RequestID(r)
	traceID := ardahttp.TraceID(r)
	r.Header.Set(ardahttp.HeaderRequestID, requestID)
	if h.policy == nil {
		if h.logger != nil {
			h.logger.Error("proxy policy is not configured", "path", r.URL.Path, "method", r.Method)
		}
		respondRequestError(w, r, http.StatusServiceUnavailable, "policy_unavailable")
		return
	}
	match, err := h.policy.Match(r.URL.Path, r.Method)
	if err != nil || match == nil || match.Route == nil {
		if h.logger != nil {
			h.logger.Warn("proxy route denied", "path", r.URL.Path, "method", r.Method, "reason", "no policy match")
		}
		respondRequestError(w, r, http.StatusNotFound, "route_not_found")
		return
	}
	baseURL := h.upstreamBaseURL(r.URL.Path)
	if strings.TrimSpace(baseURL) == "" {
		if h.logger != nil {
			h.logger.Error("proxy route has no configured upstream", "path", r.URL.Path)
		}
		respondRequestError(w, r, http.StatusServiceUnavailable, "upstream_not_configured")
		return
	}
	target := baseURL + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	// Stream the request body to the upstream instead of buffering it: uploads
	// accept up to 100MB and io.ReadAll would hold that in gateway memory.
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		respondRequestError(w, r, http.StatusBadGateway, "proxy_request_invalid")
		return
	}
	if r.ContentLength >= 0 {
		proxyReq.ContentLength = r.ContentLength
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			proxyReq.Header.Add(k, v)
		}
	}
	proxyReq.Header.Set(ardahttp.HeaderRequestID, requestID)
	serviceAudience := ""
	if strings.HasPrefix(r.URL.Path, "/api/ai/") || strings.HasPrefix(r.URL.Path, "/api/rag/") {
		serviceAudience = "ai-service"
	}
	if serviceAudience != "" {
		serviceToken, err := identity.Issue(h.cfg.ServiceAuthSecret, "auth-gateway", serviceAudience, time.Now(), time.Minute)
		if err != nil {
			respondRequestError(w, r, http.StatusBadGateway, "ai_runtime_identity_unavailable")
			return
		}
		proxyReq.Header.Set(identity.MetadataKey, serviceToken)
	}
	if traceID != "" {
		proxyReq.Header.Set(ardahttp.HeaderTraceID, traceID)
	}
	stripAuthContextHeaders(proxyReq.Header)
	sessionID := h.readSessionCookie(r)
	var sess *session.Session
	var sessionDuration time.Duration
	if sessionID != "" {
		sessionStart := time.Now()
		sess, _ = h.store.Get(r.Context(), sessionID)
		sessionDuration = time.Since(sessionStart)
	}
	requireAuth := match == nil || match.RequireAuth
	if requireAuth && sess == nil {
		h.logProxyDenied(r, requestID, traceID, http.StatusUnauthorized, "not_authenticated", nil)
		h.clearSessionCookie(w)
		respondRequestError(w, r, http.StatusUnauthorized, "not_authenticated")
		return
	}
	var ensureDuration time.Duration
	if sess != nil {
		if !h.recentAuthOK(r, sess) {
			h.logProxyDenied(r, requestID, traceID, http.StatusForbidden, "recent_auth_required", sess)
			respondRequestError(w, r, http.StatusForbidden, "recent_auth_required")
			return
		}
		forceFreshUser := match != nil && match.Route.Risk == "high"
		ensureStart := time.Now()
		if !h.ensureSessionUser(r.Context(), sess, forceFreshUser) {
			ensureDuration = time.Since(ensureStart)
			if requireAuth {
				h.logProxyDenied(r, requestID, traceID, http.StatusUnauthorized, "user_context_unavailable", sess)
				h.clearSessionCookie(w)
				respondRequestError(w, r, http.StatusUnauthorized, "user_context_unavailable")
				return
			}
		} else {
			ensureDuration = time.Since(ensureStart)
			h.maybeRenewSession(w, r.Context(), sess)
			h.touchSessionActivity(r.Context(), sess)
			if match != nil && len(match.Route.Permissions) > 0 && !sess.User.IsGlobalAdmin && !permission.HasAny(sess.User.Permissions, match.Route.Permissions...) {
				h.logProxyDenied(r, requestID, traceID, http.StatusForbidden, "insufficient_permissions", sess)
				respondRequestError(w, r, http.StatusForbidden, "insufficient_permissions")
				return
			}
			proxyReq.Header.Set("Authorization", "Bearer "+sess.AccessToken)
			proxyReq.Header.Set("X-User-Id", sess.User.UserID)
			// X-User-Id is the authenticated actor. Keep an explicit actor
			// header for downstream audit/command code; target resources remain
			// request-specific IDs and must never be inferred from this header.
			proxyReq.Header.Set("X-Actor-User-Id", sess.User.UserID)
			proxyReq.Header.Set("X-User-Subject", sess.User.Subject)
			proxyReq.Header.Set("X-Username", sess.User.Username)
			proxyReq.Header.Set("X-User-Email", sess.User.Email)
			proxyReq.Header.Set("X-Nickname", sess.User.Nickname)
			proxyReq.Header.Set("X-Tenant-Id", sess.User.TenantID)
			// Business timezone of the session user (IANA, from iam profile);
			// downstream services resolve "today"/date ranges per request.
			if sess.User.Timezone != "" {
				proxyReq.Header.Set(ardahttp.HeaderUserTimezone, sess.User.Timezone)
			}
			if len(sess.User.OrgIDs) > 0 {
				proxyReq.Header.Set("X-User-Org-Ids", strings.Join(sess.User.OrgIDs, ","))
			}
			if len(sess.User.GroupIDs) > 0 {
				proxyReq.Header.Set("X-User-Group-Ids", strings.Join(sess.User.GroupIDs, ","))
			}
			if activeOrg := strings.TrimSpace(r.Header.Get("X-Org-Id")); activeOrg != "" {
				if !containsString(sess.User.OrgIDs, activeOrg) {
					respondRequestError(w, r, http.StatusForbidden, "organization_forbidden")
					return
				}
				proxyReq.Header.Set("X-Org-Id", activeOrg)
			}
			proxyReq.Header.Set("X-Roles", strings.Join(sess.User.Roles, ","))
			proxyReq.Header.Set("X-Permissions", strings.Join(sess.User.Permissions, ","))
			proxyReq.Header.Set("X-Global-Roles", strings.Join(sess.User.GlobalRoles, ","))
			proxyReq.Header.Set("X-Global-Permissions", strings.Join(sess.User.GlobalPermissions, ","))
			if sess.User.IsGlobalAdmin {
				proxyReq.Header.Set("X-Global-Admin", "true")
			}
			proxyReq.Header.Set("X-Auth-Version", fmt.Sprintf("%d", sess.User.AuthVersion))
			if !sess.AuthTime.IsZero() {
				proxyReq.Header.Set("X-Auth-Time", fmt.Sprintf("%d", sess.AuthTime.Unix()))
			}
			if sess.IAMSessionID != "" {
				proxyReq.Header.Set("X-Session-Id", sess.IAMSessionID)
			}
			if match != nil {
				proxyReq.Header.Set("X-Auth-Risk", match.Route.Risk)
			}
			proxyReq.Header.Set("X-Auth-Checked", "true")
		}
	}
	client := h.httpClient
	if isEventStreamRequest(r) {
		client = h.streamHTTPClient
	}
	if client == nil {
		client = http.DefaultClient
	}
	proxyReq, cancelStream := withSessionStreamDeadline(proxyReq, isEventStreamRequest(r), sess)
	defer cancelStream()
	upstreamStart := time.Now()
	resp, err := client.Do(proxyReq)
	upstreamDuration := time.Since(upstreamStart)
	if err != nil {
		h.logger.Error("upstream proxy failed",
			"method", r.Method,
			"path", r.URL.Path,
			"upstream", baseURL,
			"duration_ms", upstreamDuration.Milliseconds(),
			"err", err,
		)
		respondRequestError(w, r, http.StatusBadGateway, "upstream_error")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		h.logger.Warn("upstream proxy returned error status",
			"method", r.Method,
			"path", r.URL.Path,
			"upstream", baseURL,
			"status", resp.StatusCode,
			"request_id", requestID,
			"trace_id", traceID,
			"duration_ms", upstreamDuration.Milliseconds(),
		)
	}
	for k, vs := range resp.Header {
		// The public boundary owns correlation headers. Never let an upstream
		// service replace the request ID/trace ID that the browser and gateway
		// already use; doing so makes the response body/header impossible to
		// correlate in logs and was the source of the previously observed
		// request_id mismatch.
		if strings.EqualFold(k, ardahttp.HeaderRequestID) ||
			strings.EqualFold(k, ardahttp.HeaderTraceID) ||
			strings.EqualFold(k, ardahttp.HeaderTraceParent) {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	ardahttp.SetRequestCorrelation(w, r)
	w.WriteHeader(resp.StatusCode)
	if isEventStreamRequest(r) {
		copyEventStream(w, resp.Body)
		h.logProxyAccess(r, requestID, traceID, resp.StatusCode, time.Since(start), sess, 0)
		return
	}
	copyStart := time.Now()
	bytesCopied, copyErr := io.Copy(w, resp.Body)
	copyDuration := time.Since(copyStart)
	totalDuration := time.Since(start)
	if slowLogEnabled && totalDuration >= slowLogThreshold {
		h.logger.Warn("slow proxy request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", resp.StatusCode,
			"upstream", baseURL,
			"session_ms", sessionDuration.Milliseconds(),
			"ensure_user_ms", ensureDuration.Milliseconds(),
			"upstream_ms", upstreamDuration.Milliseconds(),
			"copy_ms", copyDuration.Milliseconds(),
			"bytes", bytesCopied,
			"total_ms", totalDuration.Milliseconds(),
			"copy_err", copyErr,
		)
	}
	h.logProxyAccess(r, requestID, traceID, resp.StatusCode, totalDuration, sess, bytesCopied)
}

// logProxyAccess records every proxied request. Denials and upstream errors
// were already logged, but without a success line a lost-cookie incident
// cannot be reconstructed from the gateway's own logs (2026-09-16).
func (h *BFFHandler) logProxyAccess(r *http.Request, requestID, traceID string, status int, duration time.Duration, sess *session.Session, bytes int64) {
	if h.logger == nil {
		return
	}
	attrs := []any{
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", duration.Milliseconds(),
		"session_present", sess != nil,
		"request_id", requestID,
		"trace_id", traceID,
	}
	if bytes > 0 {
		attrs = append(attrs, "bytes", bytes)
	}
	if sess != nil && sess.User != nil {
		attrs = append(attrs, "user_id", sess.User.UserID, "tenant_id", sess.User.TenantID)
	}
	h.logger.Info("proxy request", attrs...)
}

func (h *BFFHandler) logProxyDenied(r *http.Request, requestID, traceID string, status int, reason string, sess *session.Session) {
	if h.logger == nil {
		return
	}
	attrs := []any{
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"reason", reason,
		"request_id", requestID,
		"trace_id", traceID,
		"session_present", sess != nil,
	}
	if sess != nil && sess.User != nil {
		attrs = append(attrs, "user_id", sess.User.UserID, "tenant_id", sess.User.TenantID)
	}
	h.logger.Warn("proxy request denied", attrs...)
}

func isEventStreamRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func withSessionStreamDeadline(req *http.Request, isStream bool, sess *session.Session) (*http.Request, context.CancelFunc) {
	if !isStream || sess == nil || sess.ExpiresAt.IsZero() {
		return req, func() {}
	}
	// Keep a long-lived stream bound to the authenticated session lifetime.
	ctx, cancel := context.WithDeadline(req.Context(), sess.ExpiresAt)
	return req.WithContext(ctx), cancel
}

func copyEventStream(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func (h *BFFHandler) upstreamBaseURL(path string) string {
	for _, route := range []struct {
		prefix string
		url    string
	}{
		{"/api/admin", h.cfg.IAMServiceURL},
		{"/api/iam", h.cfg.IAMServiceURL},
		{"/api/identity", h.cfg.IAMServiceURL},
		{"/api/platform", h.cfg.PlatformServiceURL},
		{"/api/finance", h.cfg.FinanceServiceURL},
		{"/api/media", h.cfg.MediaServiceURL},
		{"/api/documents", h.cfg.DocumentServiceURL},
		{"/api/workflow", h.cfg.WorkflowServiceURL},
		{"/api/crm", h.cfg.CRMServiceURL},
		{"/api/hrm", h.cfg.HRMServiceURL},
		{"/api/notifications", h.cfg.NotificationURL},
		{"/api/mdm", h.cfg.MDMServiceURL},
		{"/api/ai", h.cfg.AIServiceURL},
		{"/api/rag", h.cfg.RAGServiceURL},
		{"/api/loan", h.cfg.LoanServiceURL},
		{"/api/deposit", h.cfg.DepositServiceURL},
		{"/api/capital", h.cfg.CapitalServiceURL},
		{"/api/statistical", h.cfg.StatisticalServiceURL},
	} {
		if strings.HasPrefix(path, route.prefix) && strings.TrimSpace(route.url) != "" {
			return route.url
		}
	}
	return ""
}

func stripAuthContextHeaders(header http.Header) {
	for _, key := range []string{
		"X-User-Id",
		"X-Actor-User-Id",
		"X-Target-User-Id",
		"X-User-Subject",
		"X-Username",
		"X-User-Email",
		"X-Nickname",
		"X-Tenant-Id",
		"X-Org-Id",
		"X-User-Org-Ids",
		"X-Roles",
		"X-Permissions",
		"X-Global-Roles",
		"X-Global-Permissions",
		"X-Global-Admin",
		"X-User-Group-Ids",
		"X-Session-Id",
		"X-Auth-Version",
		"X-Auth-Time",
		"X-Auth-Risk",
		"X-Auth-Checked",
		"X-User-Timezone",
	} {
		header.Del(key)
	}
}
