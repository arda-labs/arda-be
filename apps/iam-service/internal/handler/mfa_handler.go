package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
)

// MFAHandler exposes MFA enrollment and verification endpoints.
type MFAHandler struct {
	svc      *service.MFAService
	userRepo *repository.UserRepository
}

// NewMFAHandler creates an MFA handler.
func NewMFAHandler(svc *service.MFAService, userRepo *repository.UserRepository) *MFAHandler {
	return &MFAHandler{svc: svc, userRepo: userRepo}
}

// ── Enrollment ──

// GenerateSecret generates a TOTP secret for a user.
// POST /api/iam/me/mfa/enroll
func (h *MFAHandler) GenerateSecret(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondCanonicalError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	username := r.Header.Get("X-Username")
	email := r.Header.Get("X-User-Email")

	secret, err := h.svc.GenerateSecret(r.Context(), userID, username, email)
	if err != nil {
		respondCanonicalError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondCanonicalJSON(w, r, http.StatusOK, map[string]any{
		"secret":      secret.Secret,
		"otpauth_url": secret.OTPAuth,
	})
}

// VerifyEnroll confirms TOTP enrollment with a code.
// POST /api/iam/me/mfa/verify-enroll
func (h *MFAHandler) VerifyEnroll(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondCanonicalError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondCanonicalError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" {
		respondCanonicalError(w, r, http.StatusBadRequest, "code required")
		return
	}

	backupCodes, err := h.svc.VerifyAndEnroll(r.Context(), userID, req.Code)
	if err != nil {
		respondCanonicalError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondCanonicalJSON(w, r, http.StatusOK, map[string]any{
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
		respondCanonicalError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	settings, err := h.svc.GetSettings(r.Context(), userID)
	if err != nil {
		respondCanonicalError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondCanonicalJSON(w, r, http.StatusOK, map[string]any{
		"is_enrolled": settings != nil && settings.IsEnrolled,
		"method": func() string {
			if settings != nil {
				return settings.Method
			}
			return ""
		}(),
	})
}

// CheckMFA reports whether the current device must complete MFA during login.
// POST /internal/iam/users/{id}/mfa/check
func (h *MFAHandler) CheckMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	result, err := h.svc.CheckMFA(r.Context(), userID, r.Header.Get("X-Device-Token"))
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
		"requires_mfa": result.RequiresMFA,
		"can_use_mfa":  result.CanUseMFA,
		"methods":      result.Methods,
	})
}

// ── Admin ──

// ResetMyMFA removes MFA enrollment for the current user.
// POST /api/iam/me/mfa/reset
func (h *MFAHandler) ResetMyMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		respondCanonicalError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}

	if err := h.svc.ResetMFA(r.Context(), userID); err != nil {
		respondCanonicalError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondCanonicalJSON(w, r, http.StatusOK, map[string]string{"status": "reset"})
}

// AdminResetMFA resets MFA enrollment for a user (admin only).
// POST /api/admin/users/{id}/mfa/reset
func (h *MFAHandler) AdminResetMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if user, err := h.userRepo.GetUserByIDScoped(r.Context(), userID, tenantID); err != nil || user == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}

	if err := h.svc.ResetMFA(r.Context(), userID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "reset"})
}

// VerifyCode verifies a TOTP code for the current MFA flow.
// POST /api/iam/me/mfa/verify
//
// The verification subject is always the gateway-verified actor. A user_id in
// the body is only accepted when it matches that actor: the login-time
// (pre-session) flow goes through the service-authenticated internal route.
func (h *MFAHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID       string `json:"user_id"`
		UserIDLegacy string `json:"userId"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondCanonicalError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	userID, ok := mfaActor(w, r, firstNonEmpty(req.UserID, req.UserIDLegacy))
	if !ok {
		return
	}
	if req.Code == "" {
		respondCanonicalError(w, r, http.StatusBadRequest, "code required")
		return
	}

	if err := h.svc.VerifyCode(r.Context(), userID, req.Code); err != nil {
		respondMFAVerifyError(w, r, err)
		return
	}

	respondCanonicalJSON(w, r, http.StatusOK, map[string]string{"status": "verified"})
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
		respondCanonicalError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	userID, ok := mfaActor(w, r, firstNonEmpty(req.UserID, req.UserIDLegacy))
	if !ok {
		return
	}
	if req.Code == "" {
		respondCanonicalError(w, r, http.StatusBadRequest, "backup_code required")
		return
	}

	if err := h.svc.VerifyBackupCode(r.Context(), userID, req.Code); err != nil {
		respondMFAVerifyError(w, r, err)
		return
	}

	respondCanonicalJSON(w, r, http.StatusOK, map[string]string{"status": "verified"})
}

// InternalVerifyCode verifies a TOTP code for a server-authenticated caller
// during login, before any browser session exists. The subject is taken from
// the path and the route is gated by the auth-gateway service identity.
// POST /internal/iam/users/{id}/mfa/verify
func (h *MFAHandler) InternalVerifyCode(w http.ResponseWriter, r *http.Request) {
	h.internalVerify(w, r, func(userID, code string) error {
		return h.svc.VerifyCode(r.Context(), userID, code)
	}, "code")
}

// InternalVerifyBackupCode verifies a backup code for a server-authenticated
// caller during login.
// POST /internal/iam/users/{id}/mfa/verify-backup
func (h *MFAHandler) InternalVerifyBackupCode(w http.ResponseWriter, r *http.Request) {
	h.internalVerify(w, r, func(userID, code string) error {
		return h.svc.VerifyBackupCode(r.Context(), userID, code)
	}, "backup_code")
}

func (h *MFAHandler) internalVerify(w http.ResponseWriter, r *http.Request, verify func(userID, code string) error, codeField string) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	var req struct {
		Code       string `json:"code"`
		BackupCode string `json:"backup_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	code := req.Code
	if codeField == "backup_code" {
		code = req.BackupCode
	}
	if strings.TrimSpace(code) == "" {
		respondError(w, r, http.StatusBadRequest, codeField+" required")
		return
	}
	if err := verify(userID, code); err != nil {
		respondMFAVerifyError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "verified"})
}

// mfaActor resolves the gateway-verified actor for self-service MFA routes and
// validates any user id claimed in the body against it.
func mfaActor(w http.ResponseWriter, r *http.Request, claimed string) (string, bool) {
	actor := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if actor == "" {
		respondCanonicalError(w, r, http.StatusUnauthorized, "verified actor is required")
		return "", false
	}
	if claimed = strings.TrimSpace(claimed); claimed != "" && claimed != actor {
		respondCanonicalError(w, r, http.StatusForbidden, "user_id does not match the verified actor")
		return "", false
	}
	return actor, true
}

// respondMFAVerifyError maps verification failures; a locked account is a 429
// so clients back off instead of hammering the endpoint.
func respondMFAVerifyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, service.ErrMFALocked) {
		respondCanonicalError(w, r, http.StatusTooManyRequests, err.Error())
		return
	}
	respondCanonicalError(w, r, http.StatusUnauthorized, err.Error())
}
