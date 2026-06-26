package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
)

type PlatformHandler struct {
	repo *repository.PlatformRepository
}

func NewPlatformHandler(repo *repository.PlatformRepository) *PlatformHandler {
	return &PlatformHandler{repo: repo}
}

func (h *PlatformHandler) ListParameters(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListParameters(r.Context(), r.URL.Query().Get("tenant_id"), r.URL.Query().Get("scope_type"), r.URL.Query().Get("scope_id"))
	writeResult(w, items, err)
}

func (h *PlatformHandler) UpsertParameter(w http.ResponseWriter, r *http.Request) {
	var req domain.Parameter
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Key == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "key and value are required")
		return
	}
	item, err := h.repo.UpsertParameter(r.Context(), req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListLookupCategories(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListLookupCategories(r.Context(), r.URL.Query().Get("tenant_id"), r.URL.Query().Get("scope_type"), r.URL.Query().Get("scope_id"))
	writeResult(w, items, err)
}

func (h *PlatformHandler) UpsertLookupCategory(w http.ResponseWriter, r *http.Request) {
	var req domain.LookupCategory
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}
	item, err := h.repo.UpsertLookupCategory(r.Context(), req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListLookupValues(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimPrefix(r.URL.Path, "/api/platform/lookups/")
	category = strings.TrimSuffix(category, "/values")
	items, err := h.repo.ListLookupValues(r.Context(), category)
	writeResult(w, items, err)
}

func (h *PlatformHandler) CreateLookupValue(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimPrefix(r.URL.Path, "/api/platform/lookups/")
	category = strings.TrimSuffix(category, "/values")
	var req domain.LookupValue
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}
	item, err := h.repo.CreateLookupValue(r.Context(), category, req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListOrganizations(r.Context(), r.URL.Query().Get("tenant_id"))
	writeResult(w, items, err)
}

func (h *PlatformHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req domain.Organization
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}
	item, err := h.repo.CreateOrganization(r.Context(), req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) ListGeoAdminUnits(w http.ResponseWriter, r *http.Request) {
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	items, err := h.repo.ListGeoAdminUnits(r.Context(), r.URL.Query().Get("parent_code"), level)
	writeResult(w, items, err)
}

func (h *PlatformHandler) UpsertGeoAdminUnit(w http.ResponseWriter, r *http.Request) {
	var req domain.GeoAdminUnit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" || req.Level == 0 || req.UnitType == "" {
		writeError(w, http.StatusBadRequest, "code, name, level and unit_type are required")
		return
	}
	item, err := h.repo.UpsertGeoAdminUnit(r.Context(), req)
	writeResult(w, item, err)
}

func writeResult(w http.ResponseWriter, data any, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, `{"error":"encode response"}`, http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
