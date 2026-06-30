package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/policy"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/token"
	"github.com/arda-labs/arda/libs/go/arda-auth/jwtverifier"
	"github.com/arda-labs/arda/libs/go/arda-auth/permission"
)

// AuthHandler implements the ForwardAuth endpoint.
type AuthHandler struct {
	verifier token.Verifier
	iam      *iamclient.Client
	policy   *policy.Policy
	logger   *slog.Logger
	cache    *userContextCache
}

// NewAuthHandler creates the auth check handler.
func NewAuthHandler(verifier token.Verifier, iam *iamclient.Client, pol *policy.Policy, logger *slog.Logger, cacheTTL time.Duration) *AuthHandler {
	return &AuthHandler{
		verifier: verifier,
		iam:      iam,
		policy:   pol,
		logger:   logger,
		cache:    newUserContextCache(cacheTTL),
	}
}

// Check evaluates the request and returns 200/401/403 with user headers.
func (h *AuthHandler) Check(w http.ResponseWriter, r *http.Request) {
	forwardedURI := r.Header.Get("X-Forwarded-Uri")
	forwardedMethod := r.Header.Get("X-Forwarded-Method")
	if forwardedURI == "" {
		forwardedURI = r.URL.Path
	}
	if forwardedMethod == "" {
		forwardedMethod = r.Method
	}

	match, err := h.policy.Match(forwardedURI, forwardedMethod)
	if err != nil {
		h.logger.Warn("route denied", "path", forwardedURI, "method", forwardedMethod, "reason", "no policy match")
		h.respondDenied(w, "route not allowed")
		return
	}

	h.logger.Debug("route matched", "id", match.Route.ID, "path", forwardedURI, "method", forwardedMethod)

	if match.Public {
		w.WriteHeader(http.StatusOK)
		return
	}

	raw := jwtverifier.ExtractBearer(r.Header.Get("Authorization"))
	if raw == "" {
		h.logger.Warn("unauthorized", "path", forwardedURI, "reason", "missing token")
		h.respondDenied(w, "missing authorization")
		return
	}

	claims, err := h.verifier.Verify(r.Context(), raw)
	if err != nil {
		h.logger.Warn("unauthorized", "path", forwardedURI, "reason", err.Error())
		h.respondDenied(w, "invalid token")
		return
	}

	ctx, err := h.resolveUserContext(r, claims.Subject, match.Route.Risk)
	if err != nil {
		h.logger.Warn("unauthorized", "path", forwardedURI, "subject", claims.Subject, "reason", err.Error())
		h.respondDenied(w, "user context unavailable")
		return
	}

	if len(match.Route.Permissions) > 0 && !permission.HasAny(ctx.Permissions, match.Route.Permissions...) {
		h.logger.Warn("forbidden", "path", forwardedURI, "subject", claims.Subject, "route", match.Route.ID)
		h.respondForbidden(w)
		return
	}

	h.injectHeaders(w, ctx, match.Route.Risk)
	w.WriteHeader(http.StatusOK)
}

func (h *AuthHandler) injectHeaders(w http.ResponseWriter, ctx *iamclient.UserContext, risk string) {
	w.Header().Set("X-User-Id", ctx.UserID)
	w.Header().Set("X-User-Subject", ctx.Subject)
	w.Header().Set("X-Username", ctx.Username)
	w.Header().Set("X-User-Email", ctx.Email)
	w.Header().Set("X-Nickname", ctx.Nickname)
	w.Header().Set("X-Tenant-Id", ctx.TenantID)
	w.Header().Set("X-Roles", strings.Join(ctx.Roles, ","))
	w.Header().Set("X-Permissions", strings.Join(ctx.Permissions, ","))
	w.Header().Set("X-Auth-Risk", risk)
	w.Header().Set("X-Auth-Checked", "true")
}

func (h *AuthHandler) resolveUserContext(r *http.Request, subject, risk string) (*iamclient.UserContext, error) {
	if risk != "high" {
		if ctx, ok := h.cache.get(subject); ok {
			return ctx, nil
		}
	}

	ctx, err := h.iam.GetUserBySubject(r.Context(), subject)
	if err == nil {
		if risk != "high" {
			h.cache.set(subject, ctx)
		}
		return ctx, nil
	}

	ctx, byIDErr := h.iam.GetUserByID(r.Context(), subject)
	if byIDErr == nil {
		if risk != "high" {
			h.cache.set(subject, ctx)
		}
		return ctx, nil
	}

	return nil, err
}

func (h *AuthHandler) respondDenied(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

func (h *AuthHandler) respondForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"insufficient permissions"}`))
}
