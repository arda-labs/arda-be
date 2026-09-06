package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	"github.com/arda-labs/arda/apps/mdm-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func requiredTenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "verified tenant scope is required")
		return "", false
	}
	return tenantID, true
}

func writeResult(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, data)
}

func writeErrorCode(w http.ResponseWriter, status int, code, message string) {
	ardahttp.WriteProblem(w, nil, status, ardaerrors.New(code, message))
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *ardaerrors.Error
	if errors.As(err, &appErr) {
		status := http.StatusBadRequest
		if appErr.Code == ardaerrors.CodeNotFound {
			status = http.StatusNotFound
		}
		if appErr.Code == ardaerrors.CodeConflict {
			status = http.StatusConflict
		}
		ardahttp.WriteProblem(w, r, status, appErr)
		return
	}
	ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
}

// CatalogHandler serves every simple master-data catalog registered in the
// service layer through one generic implementation.
type CatalogHandler struct {
	svc *service.MasterService
}

func NewCatalogHandler(svc *service.MasterService) *CatalogHandler {
	return &CatalogHandler{svc: svc}
}

func (h *CatalogHandler) List(catalog string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := requiredTenantID(w, r)
		if !ok {
			return
		}
		includeInactive := r.URL.Query().Get("include_inactive") == "true"
		items, err := h.svc.List(r.Context(), catalog, tenantID, includeInactive)
		writeResult(w, r, items, err)
	}
}

func (h *CatalogHandler) Get(catalog string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := requiredTenantID(w, r)
		if !ok {
			return
		}
		item, err := h.svc.Get(r.Context(), catalog, tenantID, r.PathValue("id"))
		writeResult(w, r, item, err)
	}
}

func (h *CatalogHandler) Create(catalog string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := requiredTenantID(w, r)
		if !ok {
			return
		}
		var item domain.CatalogItem
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
			return
		}
		item.ID = ""
		created, err := h.svc.Create(r.Context(), catalog, tenantID, item)
		writeResult(w, r, created, err)
	}
}

func (h *CatalogHandler) Update(catalog string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := requiredTenantID(w, r)
		if !ok {
			return
		}
		var item domain.CatalogItem
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
			return
		}
		item.ID = ""
		updated, err := h.svc.Update(r.Context(), catalog, tenantID, r.PathValue("id"), item)
		writeResult(w, r, updated, err)
	}
}

func (h *CatalogHandler) Delete(catalog string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := requiredTenantID(w, r)
		if !ok {
			return
		}
		err := h.svc.Delete(r.Context(), catalog, tenantID, r.PathValue("id"))
		writeResult(w, r, json.RawMessage(`{"deleted":true}`), err)
	}
}
