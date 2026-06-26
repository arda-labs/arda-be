package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

type PlatformHandler struct {
	svc *service.PlatformService
}

func NewPlatformHandler(svc *service.PlatformService) *PlatformHandler {
	return &PlatformHandler{svc: svc}
}

func (h *PlatformHandler) ListParameters(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListParameters(r.Context(), r.URL.Query().Get("tenant_id"), r.URL.Query().Get("scope_type"), r.URL.Query().Get("scope_id"))
	writeResult(w, items, err)
}

func (h *PlatformHandler) UpsertParameter(w http.ResponseWriter, r *http.Request) {
	var req domain.Parameter
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	if req.Key == "" || req.Value == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "key and value are required")
		return
	}
	item, err := h.svc.UpsertParameter(r.Context(), req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListLookupCategories(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListLookupCategories(r.Context(), r.URL.Query().Get("tenant_id"), r.URL.Query().Get("scope_type"), r.URL.Query().Get("scope_id"))
	writeResult(w, items, err)
}

func (h *PlatformHandler) UpsertLookupCategory(w http.ResponseWriter, r *http.Request) {
	var req domain.LookupCategory
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code and name are required")
		return
	}
	item, err := h.svc.UpsertLookupCategory(r.Context(), req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListLookupValues(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimPrefix(r.URL.Path, "/api/platform/lookups/")
	category = strings.TrimSuffix(category, "/values")
	items, err := h.svc.ListLookupValues(r.Context(), category)
	writeResult(w, items, err)
}

func (h *PlatformHandler) CreateLookupValue(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimPrefix(r.URL.Path, "/api/platform/lookups/")
	category = strings.TrimSuffix(category, "/values")
	var req domain.LookupValue
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code and name are required")
		return
	}
	item, err := h.svc.UpsertLookupValue(r.Context(), category, req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListOrganizations(r.Context(), r.URL.Query().Get("tenant_id"))
	writeResult(w, items, err)
}

func (h *PlatformHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req domain.Organization
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code and name are required")
		return
	}
	item, err := h.svc.CreateOrganization(r.Context(), req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListGeoAdminUnits(w http.ResponseWriter, r *http.Request) {
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	items, err := h.svc.ListGeoAdminUnits(r.Context(), r.URL.Query().Get("parent_code"), level)
	writeResult(w, items, err)
}

func (h *PlatformHandler) UpsertGeoAdminUnit(w http.ResponseWriter, r *http.Request) {
	var req domain.GeoAdminUnit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" || req.Level == 0 || req.UnitType == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code, name, level and unit_type are required")
		return
	}
	item, err := h.svc.UpsertGeoAdminUnit(r.Context(), req)
	writeResult(w, item, err)
}

func writeResult(w http.ResponseWriter, data any, err error) {
	if err != nil {
		var appErr *ardaerrors.Error
		if errors.As(err, &appErr) {
			writeErrorCode(w, http.StatusBadRequest, appErr.Code, appErr.Message)
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, "common.error.internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, `{"error":"encode response"}`, http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeErrorCode(w, status, "common.error.unknown", message)
}

func writeErrorCode(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
