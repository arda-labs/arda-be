package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/iam-service/internal/service"
)

// SessionHandler exposes session management endpoints.
type SessionHandler struct {
	svc *service.SessionService
}

// NewSessionHandler creates a session handler.
func NewSessionHandler(svc *service.SessionService) *SessionHandler {
	return &SessionHandler{svc: svc}
}

// ── User self-service ──

// ListMySessions returns current user's active sessions.
// GET /api/iam/me/sessions
func (h *SessionHandler) ListMySessions(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	sessions, err := h.svc.ListSessions(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "list sessions failed")
		return
	}

	resp := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		resp = append(resp, map[string]any{
			"id":         s.ID,
			"deviceId":   s.DeviceID,
			"ipAddress":  s.IPAddress,
			"userAgent":  s.UserAgent,
			"createdAt":  s.CreatedAt,
			"lastSeenAt": s.LastSeenAt,
			"expiresAt":  s.ExpiresAt,
			"isActive":   s.IsActive,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"sessions": resp})
}

// RevokeMySession revokes a specific session.
// DELETE /api/iam/me/sessions/{id}
func (h *SessionHandler) RevokeMySession(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "missing session id")
		return
	}

	if err := h.svc.RevokeSession(r.Context(), sessionID, userID, "user_revoked"); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// RevokeMyOtherSessions revokes all sessions except current.
// DELETE /api/iam/me/sessions?keep=<current_session_id>
func (h *SessionHandler) RevokeMyOtherSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	keepID := r.URL.Query().Get("keep")
	if keepID == "" {
		respondError(w, http.StatusBadRequest, "missing ?keep= parameter")
		return
	}

	n, err := h.svc.RevokeAllExcept(r.Context(), userID, keepID, "user_revoked_others")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": n})
}

// ── Device handlers ──

// ListMyDevices returns current user's devices.
// GET /api/iam/me/devices
func (h *SessionHandler) ListMyDevices(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	devices, err := h.svc.ListDevices(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "list devices failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// DeleteMyDevice removes a device (and revokes its sessions).
// DELETE /api/iam/me/devices/{id}
func (h *SessionHandler) DeleteMyDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	deviceID := r.PathValue("id")
	if deviceID == "" {
		respondError(w, http.StatusBadRequest, "missing device id")
		return
	}

	if err := h.svc.DeleteDevice(r.Context(), deviceID, userID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TrustMyDevice marks a device as trusted (for MFA skip).
// POST /api/iam/me/devices/{id}/trust
func (h *SessionHandler) TrustMyDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	deviceID := r.PathValue("id")
	if deviceID == "" {
		respondError(w, http.StatusBadRequest, "missing device id")
		return
	}

	if err := h.svc.TrustDevice(r.Context(), deviceID, userID, true); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "trusted"})
}

// SessionConfig returns the session policy config.
// GET /api/iam/session/config
func (h *SessionHandler) SessionConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.svc.GetConfig()
	respondJSON(w, http.StatusOK, cfg)
}

// ── Internal API (service-to-service, called by auth-gateway) ──

type internalCreateSessionRequest struct {
	UserID          string `json:"userId"`
	HydraSessionID  string `json:"hydraSessionId"`
	AccessTokenJTI  string `json:"jti"`
	RefreshTokenJTI string `json:"refreshJti"`
	IPAddress       string `json:"ip"`
	UserAgent       string `json:"userAgent"`
	DeviceName      string `json:"deviceName"`
	DeviceType      string `json:"deviceType"`
	OS              string `json:"os"`
	Browser         string `json:"browser"`
	Fingerprint     string `json:"fingerprint"`
	DeviceID        string `json:"deviceId,omitempty"`
}

// InternalCreateSession is called by auth-gateway after login to create IAM session record.
// POST /internal/iam/sessions
func (h *SessionHandler) InternalCreateSession(w http.ResponseWriter, r *http.Request) {
	var req internalCreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.UserID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}

	// Get or create device
	deviceID := req.DeviceID
	if dev, err := h.svc.GetOrCreateDevice(r.Context(), req.UserID,
		req.DeviceName, req.DeviceType, req.OS, req.Browser, req.Fingerprint); err == nil {
		deviceID = dev.ID
	} else {
		h.svc.Logger().Warn("device tracking skipped", "err", err)
	}

	// Create session (enforces concurrent limits)
	sess, err := h.svc.CreateSession(r.Context(), req.UserID, deviceID,
		req.HydraSessionID, req.AccessTokenJTI, req.RefreshTokenJTI,
		req.IPAddress, req.UserAgent)
	if err != nil {
		respondJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"sessionId": sess.ID,
		"deviceId":  deviceID,
		"expiresAt": sess.ExpiresAt,
	})
}

// InternalRevokeSession is called by auth-gateway when user logs out.
// DELETE /internal/iam/sessions/{id}
func (h *SessionHandler) InternalRevokeSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session id"})
		return
	}

	if err := h.svc.ForceRevokeSession(r.Context(), sessionID, "logout"); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// InternalListSessionByUser is called by auth-gateway to list sessions for user.
// GET /internal/iam/sessions?userId=xxx
func (h *SessionHandler) InternalListSessionByUser(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}

	sessions, err := h.svc.ListSessions(r.Context(), userID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// ── Admin handlers ──

// AdminListUserSessions returns all sessions for a user (admin only).
// GET /api/admin/users/{id}/sessions
func (h *SessionHandler) AdminListUserSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondError(w, http.StatusBadRequest, "missing user id")
		return
	}

	sessions, err := h.svc.ListSessions(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "list sessions failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// AdminRevokeUserSessions revokes all sessions for a user (admin only).
// DELETE /api/admin/users/{id}/sessions
func (h *SessionHandler) AdminRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondError(w, http.StatusBadRequest, "missing user id")
		return
	}

	reason := r.URL.Query().Get("reason")
	if reason == "" {
		reason = "admin_revoked"
	}

	n, err := h.svc.RevokeAllSessions(r.Context(), userID, reason)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": n})
}
