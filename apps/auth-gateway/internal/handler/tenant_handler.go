package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// SwitchTenant changes only the active application tenant in the BFF
// session. IAM validates membership and returns a tenant-scoped context; the
// browser never gets to set X-Tenant-Id directly.
func (h *BFFHandler) SwitchTenant(w http.ResponseWriter, r *http.Request) {
	sessionID := h.readSessionCookie(r)
	if sessionID == "" {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, _ := h.store.Get(r.Context(), sessionID)
	if sess == nil || sess.User == nil || strings.TrimSpace(sess.User.UserID) == "" {
		respondError(w, http.StatusUnauthorized, "session expired")
		return
	}
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		respondError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	if h.iamClient == nil {
		respondError(w, http.StatusBadGateway, "tenant context is not configured")
		return
	}
	uc, err := h.iamClient.GetUserByID(r.Context(), sess.User.UserID, tenantID)
	if err != nil {
		h.logger.Warn("tenant switch denied", "user_id", sess.User.UserID, "tenant_id", tenantID, "err", err)
		respondError(w, http.StatusForbidden, "tenant membership is not active")
		return
	}
	sess.User = sessionUserFromIAM(uc, sess.User)
	h.updateSession(r.Context(), sess)
	ardahttp.WriteSuccess(w, r, http.StatusOK, sess.User)
}
