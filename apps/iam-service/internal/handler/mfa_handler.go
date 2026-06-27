package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/iam-service/internal/service"
)

// MFAHandler exposes MFA enrollment and verification endpoints.
type MFAHandler struct {
	svc *service.MFAService
}

// NewMFAHandler creates an MFA handler.
func NewMFAHandler(svc *service.MFAService) *MFAHandler {
	return &MFAHandler{svc: svc}
}

// ── Enrollment ──

// GenerateSecret generates a TOTP secret for a user.
// POST /api/iam/me/mfa/enroll
func (h *MFAHandler) GenerateSecret(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	username := r.Header.Get("X-Username")
	email := r.Header.Get("X-User-Email")

	secret, err := h.svc.GenerateSecret(r.Context(), userID, username, email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"secret":      secret.Secret,
		"otpauth_url": secret.OTPAuth,
	})
}

// VerifyEnroll confirms TOTP enrollment with a code.
// POST /api/iam/me/mfa/verify-enroll
func (h *MFAHandler) VerifyEnroll(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" {
		respondError(w, http.StatusBadRequest, "code required")
		return
	}

	backupCodes, err := h.svc.VerifyAndEnroll(r.Context(), userID, req.Code)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"status":       "enrolled",
		"backup_codes": backupCodes,
	})
}

// ── Status ──

// MFAStatus returns MFA enrollment status.
// GET /api/iam/me/mfa/status
func (h *MFAHandler) MFAStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	settings, err := h.svc.GetSettings(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"is_enrolled": settings != nil && settings.IsEnrolled,
		"method": func() string {
			if settings != nil {
				return settings.Method
			}
			return ""
		}(),
	})
}

// ── Admin ──

// ResetMyMFA removes MFA enrollment for the current user.
// POST /api/iam/me/mfa/reset
func (h *MFAHandler) ResetMyMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	if err := h.svc.ResetMFA(r.Context(), userID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

// AdminResetMFA resets MFA enrollment for a user (admin only).
// POST /api/admin/users/{id}/mfa/reset
func (h *MFAHandler) AdminResetMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondError(w, http.StatusBadRequest, "missing user id")
		return
	}

	if err := h.svc.ResetMFA(r.Context(), userID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

// MFAVerification is the temporary token used during login MFA step.
type MFAVerification struct {
	UserID    string
	ExpiresAt int64
}

// VerifyCode verifies a TOTP code during login (called from orchestrator).
// POST /api/auth/login/mfa
func (h *MFAHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"userId"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.UserID == "" || req.Code == "" {
		respondError(w, http.StatusBadRequest, "userId and code required")
		return
	}

	if err := h.svc.VerifyCode(r.Context(), req.UserID, req.Code); err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "verified", "mfaToken": req.UserID + "_mfa_ok"})
}

// VerifyBackupCode verifies a backup code during login.
// POST /api/auth/login/mfa/backup
func (h *MFAHandler) VerifyBackupCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"userId"`
		Code   string `json:"backup_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.UserID == "" || req.Code == "" {
		respondError(w, http.StatusBadRequest, "userId and backup_code required")
		return
	}

	if err := h.svc.VerifyBackupCode(r.Context(), req.UserID, req.Code); err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "verified", "mfaToken": req.UserID + "_mfa_ok"})
}
