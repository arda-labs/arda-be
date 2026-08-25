package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/service"
)

type TenantHandler struct {
	svc *service.TenantService
}

func NewTenantHandler(svc *service.TenantService) *TenantHandler {
	return &TenantHandler{svc: svc}
}

func (h *TenantHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondCanonicalError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	items, err := h.svc.ListForUser(r.Context(), userID)
	if err != nil {
		respondCanonicalError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondCanonicalJSON(w, r, http.StatusOK, items)
}

func (h *TenantHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	if !hasGlobalAdminCapability(r) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, "common.error.forbidden", "global tenant administration is required")
		return
	}
	items, err := h.svc.List(r.Context())
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, items)
}

func (h *TenantHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !hasGlobalAdminCapability(r) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, "common.error.forbidden", "global tenant administration is required")
		return
	}
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		OwnerUserID string `json:"owner_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	tenant, err := h.svc.Create(r.Context(), req.Code, req.Name, req.OwnerUserID)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			status = http.StatusConflict
		}
		respondAdminError(w, r, status, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusCreated, tenant)
}

func (h *TenantHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	if !hasGlobalAdminCapability(r) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, "common.error.forbidden", "global tenant administration is required")
		return
	}
	tenantID := strings.TrimSpace(r.PathValue("tenant_id"))
	members, err := h.svc.ListMembers(r.Context(), tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, members)
}

func (h *TenantHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	if !hasGlobalAdminCapability(r) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, "common.error.forbidden", "global tenant administration is required")
		return
	}
	var req struct {
		UserID    string `json:"user_id"`
		IsDefault bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.svc.AddMember(r.Context(), r.PathValue("tenant_id"), req.UserID, req.IsDefault); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]any{"tenant_id": r.PathValue("tenant_id"), "user_id": req.UserID})
}

func (h *TenantHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	if !hasGlobalAdminCapability(r) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, "common.error.forbidden", "global tenant administration is required")
		return
	}
	if err := h.svc.RemoveMember(r.Context(), r.PathValue("tenant_id"), r.PathValue("user_id")); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]any{"tenant_id": r.PathValue("tenant_id"), "user_id": r.PathValue("user_id")})
}
