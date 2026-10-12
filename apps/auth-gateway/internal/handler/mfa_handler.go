package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

func (h *BFFHandler) verifyMFA(ctx context.Context, userID, code string) error {
	if h.iamClient == nil {
		return fmt.Errorf("iam client is not configured")
	}
	// Verify through the service-authenticated internal route. The public
	// /api/iam/me/mfa/verify route is actor-scoped (X-User-Id) and must never
	// accept a caller-declared user id, so a server-side step-up uses the same
	// client path as login MFA.
	return h.iamClient.VerifyMFA(ctx, userID, code)
}

func (h *BFFHandler) clearRememberMFACookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     rememberMFACookieName,
		Value:    "",
		Path:     "/",
		Domain:   h.cfg.SessionCookieDomain,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: parseSameSite(h.cfg.CookieSameSite),
		MaxAge:   -1,
	})
}

func (h *BFFHandler) StepUp(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil || sess.User == nil || sess.User.UserID == "" {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	var req struct {
		Code    string `json:"code"`
		Confirm bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	mfaEnrolled := h.userMFAEnrolled(r.Context(), sess.User.UserID)
	if mfaEnrolled {
		if strings.TrimSpace(req.Code) == "" {
			respondError(w, http.StatusBadRequest, "code is required")
			return
		}
		if err := h.verifyMFA(r.Context(), sess.User.UserID, strings.TrimSpace(req.Code)); err != nil {
			// Machine code (no spaces) so this stays a step-up failure and is
			// never mapped to the generic 401 "auth.error.unauthorized" — the
			// frontend treats that code as a global session loss and would
			// log the user out just for mistyping an OTP.
			respondError(w, http.StatusUnauthorized, "auth.step_up.invalid_code")
			return
		}
	} else if !req.Confirm {
		respondError(w, http.StatusBadRequest, "confirmation required")
		return
	}
	if err := h.refreshSessionAuthTime(w, r.Context(), sessionID, sess); err != nil {
		respondError(w, http.StatusInternalServerError, "refresh session failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"status":       "verified",
		"stepUpUntil":  sess.AuthTime.Add(time.Duration(h.cfg.RecentAuthWindow) * time.Second).Format(time.RFC3339),
		"validSeconds": h.cfg.RecentAuthWindow,
	})
}

func (h *BFFHandler) RecentAuthStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	recentOk := h.cfg.RecentAuthWindow <= 0 ||
		(!sess.AuthTime.IsZero() &&
			time.Since(sess.AuthTime) <= time.Duration(h.cfg.RecentAuthWindow)*time.Second)
	resp := map[string]any{
		"recentAuthOk": recentOk,
		"validSeconds": h.cfg.RecentAuthWindow,
	}
	if !sess.AuthTime.IsZero() && h.cfg.RecentAuthWindow > 0 {
		resp["stepUpUntil"] = sess.AuthTime.Add(time.Duration(h.cfg.RecentAuthWindow) * time.Second).Format(time.RFC3339)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *BFFHandler) userMFAEnrolled(ctx context.Context, userID string) bool {
	if h.iamClient == nil || userID == "" {
		return true
	}
	status, err := h.iamClient.GetMFAStatus(ctx, userID)
	if err != nil || status == nil {
		return true
	}
	return status.IsEnrolled
}

func (h *BFFHandler) refreshSessionAuthTime(w http.ResponseWriter, ctx context.Context, sessionID string, sess *session.Session) error {
	sess.AuthTime = time.Now()
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if err := h.store.Refresh(ctx, sessionID, sess, ttl); err != nil {
		return err
	}
	h.setSessionCookie(w, sess.ID, ttl)
	return nil
}

func (h *BFFHandler) recentAuthOK(r *http.Request, sess *session.Session) bool {
	if h.cfg.RecentAuthWindow <= 0 || sess == nil || h.policy == nil {
		return true
	}
	match, err := h.policy.Match(r.URL.Path, r.Method)
	if err != nil || match.Route.Risk != "high" {
		return true
	}
	if sess.AuthTime.IsZero() {
		return false
	}
	return time.Since(sess.AuthTime) <= time.Duration(h.cfg.RecentAuthWindow)*time.Second
}

func (h *BFFHandler) readRememberMFACookie(r *http.Request) bool {
	c, err := r.Cookie(rememberMFACookieName)
	if err != nil {
		return false
	}
	return c.Value == "1"
}
