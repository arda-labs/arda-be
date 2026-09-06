package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// MenuHandler serves DB-driven navigation for the MFE shell.
type MenuHandler struct {
	svc *service.MenuService
}

func NewMenuHandler(svc *service.MenuService) *MenuHandler {
	return &MenuHandler{svc: svc}
}

// GetEffectiveMenu returns the active menu tree for the verified tenant.
func (h *MenuHandler) GetEffectiveMenu(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListEffective(r.Context(), tenantID)
	writeResultWithRequest(w, r, items, err)
}

// ListMenuItems returns raw menu rows (admin listing, includes inactive).
func (h *MenuHandler) ListMenuItems(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.List(r.Context(), tenantID)
	writeResultWithRequest(w, r, items, err)
}

func (h *MenuHandler) UpsertMenuItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	var item domain.MenuItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return
	}
	saved, err := h.svc.Upsert(r.Context(), tenantID, item)
	writeResultWithRequest(w, r, saved, err)
}

func (h *MenuHandler) DeleteMenuItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), tenantID, r.PathValue("id")); err != nil {
		writeResultWithRequest(w, r, nil, err)
		return
	}
	writeResultWithRequest(w, r, json.RawMessage(`{"deleted":true}`), nil)
}
