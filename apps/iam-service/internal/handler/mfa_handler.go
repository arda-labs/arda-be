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
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	username := r.Header.Get("X-Username")
	email := r.Header.Get("X-User-Email")

	secret, err := h.svc.GenerateSecret(r.Context(), userID, username, email)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
		"secret":      secret.Secret,
		"otpauth_url": secret.OTPAuth,
	})
}

// VerifyEnroll confirms TOTP enrollment with a code.
// POST /api/iam/me/mfa/verify-enroll
func (h *MFAHandler) VerifyEnroll(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" {
		respondError(w, r, http.StatusBadRequest, "code required")
		return
	}

	backupCodes, err := h.svc.VerifyAndEnroll(r.Context(), userID, req.Code)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
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
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	settings, err := h.svc.GetSettings(r.Context(), userID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
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
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	if err := h.svc.ResetMFA(r.Context(), userID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "reset"})
}

// AdminResetMFA resets MFA enrollment for a user (admin only).
// POST /api/admin/users/{id}/mfa/reset
func (h *MFAHandler) AdminResetMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	if err := h.svc.ResetMFA(r.Context(), userID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "reset"})
}

// VerifyCode verifies a TOTP code for the current MFA flow.
// POST /api/iam/me/mfa/verify
func (h *MFAHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID       string `json:"user_id"`
		UserIDLegacy string `json:"userId"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	userID := firstNonEmpty(req.UserID, req.UserIDLegacy)
	if userID == "" || req.Code == "" {
		respondError(w, r, http.StatusBadRequest, "user_id and code required")
		return
	}

	if err := h.svc.VerifyCode(r.Context(), userID, req.Code); err != nil {
		respondError(w, r, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "verified", "mfaToken": userID + "_mfa_ok"})
}

// VerifyBackupCode verifies a backup code for the current MFA flow.
// POST /api/iam/me/mfa/backup
func (h *MFAHandler) VerifyBackupCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID       string `json:"user_id"`
		UserIDLegacy string `json:"userId"`
		Code         string `json:"backup_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	userID := firstNonEmpty(req.UserID, req.UserIDLegacy)
	if userID == "" || req.Code == "" {
		respondError(w, r, http.StatusBadRequest, "user_id and backup_code required")
		return
	}

	if err := h.svc.VerifyBackupCode(r.Context(), userID, req.Code); err != nil {
		respondError(w, r, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "verified", "mfaToken": userID + "_mfa_ok"})
}
