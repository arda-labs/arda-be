package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// sessionAbsoluteMax caps how long activity may keep sliding a session
// forward; a stolen cookie still dies eventually.
const sessionAbsoluteMax = 30 * 24 * time.Hour

// maybeRenewSession slides an active session forward once half of its TTL has
// elapsed: the browser cookie is re-issued with a fresh Max-Age and the stored
// expiry moves with it. Without this, a browser drops the cookie at the
// original 24h mark even while the user is working (2026-09-16 incident).
func (h *BFFHandler) maybeRenewSession(w http.ResponseWriter, ctx context.Context, sess *session.Session) {
	if sess == nil || sess.ID == "" || h.store == nil {
		return
	}
	ttl := time.Duration(h.cfg.SessionTTL) * time.Second
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now()
	if !sess.ExpiresAt.IsZero() && sess.ExpiresAt.Sub(now) > ttl/2 {
		return
	}
	if !sess.CreatedAt.IsZero() && now.Sub(sess.CreatedAt) > sessionAbsoluteMax {
		return
	}
	sess.ExpiresAt = now.Add(ttl)
	h.updateSession(ctx, sess)
	h.setSessionCookie(w, sess.ID, ttl)
	if h.logger != nil {
		h.logger.Info("session renewed", "session_id", sess.ID)
	}
}

// touchSessionActivity reports session activity to IAM at most once per
// interval per session, in the background so the request path is not blocked.
// It powers last_seen_at in the admin session list (2026-09-16 incident:
// records only carried the creation time).
func (h *BFFHandler) touchSessionActivity(ctx context.Context, sess *session.Session) {
	if sess == nil || sess.IAMSessionID == "" || h.iamClient == nil {
		return
	}
	now := time.Now()
	h.sessionTouchMu.Lock()
	if h.sessionTouches == nil || len(h.sessionTouches) > 1024 {
		h.sessionTouches = make(map[string]time.Time)
	}
	last, seen := h.sessionTouches[sess.IAMSessionID]
	if seen && now.Sub(last) < sessionTouchInterval {
		h.sessionTouchMu.Unlock()
		return
	}
	h.sessionTouches[sess.IAMSessionID] = now
	h.sessionTouchMu.Unlock()

	sessionID := sess.IAMSessionID
	base := context.WithoutCancel(ctx)
	go func() {
		touchCtx, cancel := context.WithTimeout(base, 3*time.Second)
		defer cancel()
		if err := h.iamClient.TouchSession(touchCtx, sessionID); err != nil && h.logger != nil {
			h.logger.Warn("touch iam session failed", "session_id", sessionID, "err", err)
		}
	}()
}

func (h *BFFHandler) updateSession(ctx context.Context, sess *session.Session) {
	if sess == nil || sess.ID == "" {
		return
	}
	ttl := time.Until(sess.ExpiresAt)
	if ttl <= 0 {
		return
	}
	if h.store == nil {
		return
	}
	if err := h.store.Update(ctx, sess, ttl); err != nil {
		if h.logger != nil {
			h.logger.Warn("update session failed", "session_id", sess.ID, "err", err)
		}
		return
	}
	if sess.User != nil && h.logger != nil {
		h.logger.Debug("session user context refreshed", "session_id", sess.ID, "user_id", sess.User.UserID, "auth_version", sess.User.AuthVersion)
	}
}

func (h *BFFHandler) Me(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	slowLogEnabled, slowLogThreshold := h.slowLogSettings()
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sessionStart := time.Now()
	sess, _ := h.store.Get(r.Context(), sessionID)
	sessionDuration := time.Since(sessionStart)
	if sess == nil {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	ensureStart := time.Now()
	if !h.ensureSessionUser(r.Context(), sess, false) {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "user context unavailable")
		return
	}
	ensureDuration := time.Since(ensureStart)
	h.maybeRenewSession(w, r.Context(), sess)
	h.touchSessionActivity(r.Context(), sess)
	writeStart := time.Now()
	ardahttp.WriteSuccess(w, r, http.StatusOK, sess.User)
	writeDuration := time.Since(writeStart)
	totalDuration := time.Since(start)
	if slowLogEnabled && totalDuration >= slowLogThreshold {
		h.logger.Warn("slow bff request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusOK,
			"session_ms", sessionDuration.Milliseconds(),
			"ensure_user_ms", ensureDuration.Milliseconds(),
			"write_ms", writeDuration.Milliseconds(),
			"total_ms", totalDuration.Milliseconds(),
		)
	}
}

func (h *BFFHandler) WebCheck(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		h.redirectToOAuthStart(w, r)
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil {
		h.clearSessionCookie(w)
		h.redirectToOAuthStart(w, r)
		return
	}
	if !h.ensureSessionUser(r.Context(), sess, false) {
		h.clearSessionCookie(w)
		h.redirectToOAuthStart(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *BFFHandler) MeSessions(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil || sess.User == nil {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	if !h.ensureSessionUser(r.Context(), sess, false) {
		h.clearSessionCookie(w)
		respondError(w, http.StatusUnauthorized, "user context unavailable")
		return
	}
	sessions, _ := h.store.ListByUser(r.Context(), sess.User.UserID)
	respondJSON(w, http.StatusOK, map[string]any{"sessions": sessionViews(sessions, sessionID), "current": sessionID})
}

// sessionView is the browser-safe projection of a BFF session. Access and
// refresh tokens stay server-side; the SPA only needs device/session metadata.
type sessionView struct {
	ID         string    `json:"id"`
	IsCurrent  bool      `json:"is_current"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	AuthTime   time.Time `json:"auth_time"`
	DeviceID   string    `json:"device_id,omitempty"`
	DeviceName string    `json:"device_name,omitempty"`
	DeviceType string    `json:"device_type,omitempty"`
	IPAddress  string    `json:"ip_address,omitempty"`
}

func sessionViews(sessions []*session.Session, currentID string) []sessionView {
	views := make([]sessionView, 0, len(sessions))
	for _, item := range sessions {
		if item == nil {
			continue
		}
		views = append(views, sessionView{
			ID:         item.ID,
			IsCurrent:  item.ID == currentID,
			CreatedAt:  item.CreatedAt,
			ExpiresAt:  item.ExpiresAt,
			AuthTime:   item.AuthTime,
			DeviceID:   item.DeviceID,
			DeviceName: item.DeviceName,
			DeviceType: item.DeviceType,
			IPAddress:  item.IPAddress,
		})
	}
	return views
}
