package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardamedia "github.com/arda-labs/arda/libs/go/arda-media"
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

func (h *PlatformHandler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	item, err := h.svc.GetOrganizationByID(r.Context(), id)
	writeResult(w, item, err)
}

func (h *PlatformHandler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	var req domain.Organization
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	req.ID = id
	if req.Code == "" || req.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code and name are required")
		return
	}
	item, err := h.svc.UpdateOrganization(r.Context(), req)
	writeResult(w, item, err)
}

func (h *PlatformHandler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	err := h.svc.DeleteOrganization(r.Context(), id)
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) DeleteParameter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	err := h.svc.DeleteParameter(r.Context(), id)
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) DeleteLookupCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	err := h.svc.DeleteLookupCategory(r.Context(), id)
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) DeleteLookupValue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	err := h.svc.DeleteLookupValue(r.Context(), id)
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) ListFileTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListFileTemplates(r.Context(), r.URL.Query().Get("tenant_id"))
	writeResult(w, items, err)
}

func (h *PlatformHandler) GetFileTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	item, err := h.svc.GetFileTemplateByID(r.Context(), id)
	writeResult(w, item, err)
}

func (h *PlatformHandler) CreateFileTemplate(w http.ResponseWriter, r *http.Request) {
	var req domain.FileTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}

	if req.Code == "" || req.Name == "" || req.FileType == "" || req.FileURL == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code, name, file_type and file_url are required")
		return
	}
	item, err := h.svc.CreateFileTemplate(r.Context(), req)
	if err == nil {
		publicID := extractPublicID(item.FileURL)
		if publicID != "" {
			if attachErr := ardamedia.NewClient().Attach(r.Context(), []string{publicID}, "file_template", item.Code, r); attachErr != nil {
				slog.Error("failed to attach template file on create", "public_id", publicID, "err", attachErr)
			}
		}
	}
	writeResult(w, item, err)
}

func (h *PlatformHandler) UpdateFileTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	var req domain.FileTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}

	req.ID = id
	if req.Code == "" || req.Name == "" || req.FileType == "" || req.FileURL == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code, name, file_type and file_url are required")
		return
	}
	item, err := h.svc.UpdateFileTemplate(r.Context(), req)
	if err == nil {
		publicID := extractPublicID(item.FileURL)
		if publicID != "" {
			if attachErr := ardamedia.NewClient().Attach(r.Context(), []string{publicID}, "file_template", item.Code, r); attachErr != nil {
				slog.Error("failed to attach template file on update", "public_id", publicID, "err", attachErr)
			}
		}
	}
	writeResult(w, item, err)
}

func (h *PlatformHandler) DeleteFileTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	err := h.svc.DeleteFileTemplate(r.Context(), id)
	writeResult(w, map[string]bool{"ok": true}, err)
}

func extractPublicID(fileURL string) string {
	parts := strings.Split(strings.TrimRight(fileURL, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if strings.HasPrefix(last, "mf_") {
		return last
	}
	return ""
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
