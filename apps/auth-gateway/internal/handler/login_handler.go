package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
)

type loginAcceptRequest struct {
	LoginChallenge     string `json:"login_challenge"`
	Subject            string `json:"subject"`
	Remember           bool   `json:"remember"`
	RememberFor        int    `json:"remember_for"`
	KratosSessionToken string `json:"kratos_session_token"`
	MFACode            string `json:"mfa_code"`
}

func (h *BFFHandler) AcceptKratosLogin(w http.ResponseWriter, r *http.Request) {
	var req loginAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LoginChallenge == "" {
		respondError(w, http.StatusBadRequest, "missing login_challenge")
		return
	}
	whoami, err := h.kratosWhoami(r, req.KratosSessionToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if h.iamClient == nil {
		respondError(w, http.StatusBadGateway, "iam client is not configured")
		return
	}
	uc, err := h.iamClient.GetUserByKratosIdentityID(r.Context(), whoami.Identity.ID)
	if err != nil {
		h.logger.Warn("resolve kratos identity failed", "identity_id", whoami.Identity.ID, "err", err)
		uc, err = h.iamClient.ResolveOrLinkKratosIdentity(r.Context(), whoami.Identity.ID, whoami.Identity.Traits.Email, whoami.Identity.Traits.Name)
		if err != nil {
			h.logger.Warn("resolve or link kratos identity failed", "identity_id", whoami.Identity.ID, "email", whoami.Identity.Traits.Email, "err", err)
			respondError(w, http.StatusBadGateway, "user context unavailable")
			return
		}
	}
	req.Subject = uc.UserID
	requiresMFA := h.requiresLoginMFA(uc)
	applyLoginRememberPolicy(&req, requiresMFA)
	if requiresMFA {
		status, err := h.iamClient.GetMFAStatus(r.Context(), uc.UserID)
		if err != nil {
			h.logger.Warn("mfa status check failed", "user_id", uc.UserID, "err", err)
			respondError(w, http.StatusBadGateway, "mfa status unavailable")
			return
		}
		if status == nil || !status.IsEnrolled {
			if strings.TrimSpace(req.MFACode) == "" {
				secret, err := h.iamClient.GenerateMFASecret(r.Context(), uc.UserID, uc.Username, uc.Email)
				if err != nil {
					h.logger.Warn("mfa enrollment secret failed", "user_id", uc.UserID, "err", err)
					respondError(w, http.StatusBadGateway, "mfa enrollment unavailable")
					return
				}
				respondJSON(w, http.StatusOK, map[string]any{
					"mfa_enrollment_required": true,
					"method":                  "totp",
					"user":                    uc.Username,
					"secret":                  secret.Secret,
					"otpauth_url":             secret.OTPAuthURL,
				})
				return
			}
			enroll, err := h.iamClient.VerifyMFAEnrollment(r.Context(), uc.UserID, strings.TrimSpace(req.MFACode))
			if err != nil {
				h.logger.Warn("mfa enrollment verify failed", "user_id", uc.UserID, "err", err)
				respondError(w, http.StatusUnauthorized, "invalid_mfa_code")
				return
			}
			redirectURL, ok := h.acceptHydraLoginURL(w, r, req, uc.UserID)
			if !ok {
				return
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"redirect_url": redirectURL,
				"backup_codes": enroll.BackupCodes,
			})
			return
		}
		if strings.TrimSpace(req.MFACode) == "" {
			deviceToken := h.readDeviceCookie(r)
			mfaCheck, err := h.iamClient.CheckMFA(r.Context(), uc.UserID, deviceToken)
			if err != nil {
				h.logger.Warn("mfa device check failed", "user_id", uc.UserID, "err", err)
				respondError(w, http.StatusBadGateway, "mfa device check unavailable")
				return
			}
			if !mfaCheck.RequiresMFA {
				h.acceptHydraLogin(w, r, req)
				return
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"mfa_required": true,
				"method":       status.Method,
				"user":         uc.Username,
			})
			return
		}
		if err := h.iamClient.VerifyMFA(r.Context(), uc.UserID, strings.TrimSpace(req.MFACode)); err != nil {
			h.logger.Warn("mfa login verify failed", "user_id", uc.UserID, "err", err)
			respondError(w, http.StatusUnauthorized, "invalid_mfa_code")
			return
		}
	}
	h.acceptHydraLogin(w, r, req)
}

func (h *BFFHandler) requiresLoginMFA(uc *iamclient.UserContext) bool {
	if uc == nil {
		return false
	}
	for _, role := range uc.Roles {
		if role == "SUPER_ADMIN" || role == "ADMIN" {
			return true
		}
	}
	for _, code := range uc.Permissions {
		if code == "superadmin" {
			return true
		}
	}
	return false
}

func applyLoginRememberPolicy(req *loginAcceptRequest, privileged bool) {
	if privileged || !req.Remember {
		req.Remember = false
		req.RememberFor = 0
		return
	}
	if req.RememberFor <= 0 || req.RememberFor > loginRememberMaxAge {
		req.RememberFor = loginRememberMaxAge
	}
}

func (h *BFFHandler) Login(w http.ResponseWriter, r *http.Request) {
	h.redirectToAuthUI(w, r, "login", "login_challenge")
}

// ValidateLoginChallenge reports whether a Hydra login challenge is still
// usable, so the login UI can re-mint a fresh flow instead of surfacing an
// expired challenge. Challenge lifecycle is Hydra state: 404/409/410 mean the
// request was consumed or expired and the whole flow must restart, while any
// other Hydra failure is a transient gateway problem.
func (h *BFFHandler) ValidateLoginChallenge(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		respondError(w, http.StatusBadRequest, "missing login_challenge")
		return
	}
	_, status, err := h.getHydraAuthRequest(r.Context(), "login", "login_challenge", challenge)
	if err == nil {
		respondJSON(w, http.StatusOK, map[string]any{"valid": true})
		return
	}
	if status == http.StatusNotFound || status == http.StatusConflict || status == http.StatusGone {
		respondJSON(w, http.StatusOK, map[string]any{"valid": false})
		return
	}
	h.logger.Warn("login challenge validation failed", "err", err)
	respondError(w, http.StatusBadGateway, "auth request unavailable")
}

func (h *BFFHandler) acceptHydraLogin(w http.ResponseWriter, r *http.Request, req loginAcceptRequest) {
	redirectURL, ok := h.acceptHydraLoginURL(w, r, req, req.Subject)
	if !ok {
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"redirect_url": redirectURL})
}

func (h *BFFHandler) acceptHydraLoginURL(w http.ResponseWriter, r *http.Request, req loginAcceptRequest, subject string) (string, bool) {
	hydraURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/login/accept?login_challenge=%s", h.cfg.HydraAdminURL, url.QueryEscape(req.LoginChallenge))
	b, _ := json.Marshal(map[string]any{"subject": subject, "remember": req.Remember, "remember_for": req.RememberFor})
	resp, err := h.doPut(r.Context(), hydraURL, "application/json", bytes.NewReader(b))
	if err != nil {
		respondError(w, http.StatusBadGateway, "accept login failed")
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusGone {
			respondError(w, http.StatusConflict, "login_challenge_expired")
			return "", false
		}
		respondError(w, resp.StatusCode, string(body))
		return "", false
	}
	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.RedirectTo == "" {
		respondError(w, http.StatusBadGateway, "accept login returned empty redirect_to")
		return "", false
	}
	return result.RedirectTo, true
}
