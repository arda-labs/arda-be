package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardamedia "github.com/arda-labs/arda/libs/go/arda-media"
)

type PlatformHandler struct {
	svc   *service.PlatformService
	media *ardamedia.Client
}

func requiredTenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "verified tenant scope is required")
		return "", false
	}
	return tenantID, true
}

func bindVerifiedTenant(w http.ResponseWriter, r *http.Request, requested *string) bool {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return false
	}
	if strings.TrimSpace(*requested) != "" && strings.TrimSpace(*requested) != tenantID {
		writeErrorCode(w, http.StatusForbidden, ardaerrors.CodeForbidden, "requested tenant is outside verified scope")
		return false
	}
	*requested = tenantID
	return true
}

func bindVerifiedTenantPointer(w http.ResponseWriter, r *http.Request, requested **string) bool {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return false
	}
	if *requested != nil && strings.TrimSpace(**requested) != "" && strings.TrimSpace(**requested) != tenantID {
		writeErrorCode(w, http.StatusForbidden, ardaerrors.CodeForbidden, "requested tenant is outside verified scope")
		return false
	}
	*requested = &tenantID
	return true
}

var organizationListSpec = ardahttp.ListSpec{
	DefaultPerPage: 20,
	MaxPerPage:     ardahttp.MaxPerPage,
	SortFields:     []string{"code", "name", "is_active", "created_at"},
	Views:          []string{"tree", "options"},
	AllowAll:       true,
	Filters: map[string]ardahttp.QueryFilterSpec{
		"is_active": ardahttp.BoolFilter(),
	},
}

func NewPlatformHandler(svc *service.PlatformService, media *ardamedia.Client) *PlatformHandler {
	return &PlatformHandler{svc: svc, media: media}
}

func (h *PlatformHandler) ListParameters(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListParameters(r.Context(), tenantID, r.URL.Query().Get("scope_type"), r.URL.Query().Get("scope_id"))
	writeResultWithRequest(w, r, items, err)
}

func (h *PlatformHandler) GetPublicBranding(w http.ResponseWriter, r *http.Request) {
	item, err := h.svc.GetGlobalParameter(r.Context(), "system.settings")
	if errors.Is(err, sql.ErrNoRows) {
		writeResultWithRequest(w, r, json.RawMessage(`{}`), nil)
		return
	}
	if err != nil {
		writeResultWithRequest(w, r, nil, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeResultWithRequest(w, r, publicBrandingPayload(item.Value), nil)
}

func publicBrandingPayload(value string) json.RawMessage {
	var settings map[string]any
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return json.RawMessage(`{}`)
	}
	out := make(map[string]any, 12)
	for _, key := range []string{
		"appName",
		"shortName",
		"organizationName",
		"supportEmail",
		"supportPhone",
		"helpUrl",
		"loginLogoUrl",
		"dashboardLogoUrl",
		"faviconUrl",
		"loginBackgroundUrl",
		"loginBackgroundEnabled",
		"loginWelcomeTitle",
		"loginWelcomeSubtitle",
	} {
		if value, ok := settings[key]; ok {
			out[key] = value
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
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
	if !bindVerifiedTenantPointer(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.UpsertParameter(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) ListLookupCategories(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListLookupCategories(r.Context(), tenantID, r.URL.Query().Get("scope_type"), r.URL.Query().Get("scope_id"))
	writeResultWithRequest(w, r, items, err)
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
	if !bindVerifiedTenantPointer(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.UpsertLookupCategory(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) ListLookupValues(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	category := strings.TrimPrefix(r.URL.Path, "/api/platform/lookups/")
	category = strings.TrimSuffix(category, "/values")
	items, err := h.svc.ListLookupValues(r.Context(), tenantID, category)
	writeResultWithRequest(w, r, items, err)
}

func (h *PlatformHandler) CreateLookupValue(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
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
	item, err := h.svc.UpsertLookupValue(r.Context(), tenantID, category, req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	listRequest, err := ardahttp.ParseListRequest(r.URL.Query(), organizationListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	listQuery := listRequest.ListQuery
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}

	unpaged := listQuery.All || listQuery.View == "tree" || listQuery.View == "options"
	items, total, err := h.svc.ListOrganizations(r.Context(), repository.ListOrganizationsParams{
		TenantID: tenantID,
		Page:     listQuery.Page,
		PerPage:  listQuery.PerPage,
		Offset:   listQuery.Offset(),
		Query:    listQuery.Q,
		IsActive: listRequest.Bool("is_active"),
		Sort:     listQuery.Sort,
		Order:    listQuery.Order,
		Unpaged:  unpaged,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	perPage := listQuery.PerPage
	if unpaged {
		perPage = len(items)
		if perPage == 0 {
			perPage = total
		}
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listQuery.Page, perPage, total, items))
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
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.CreateOrganization(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) ListGeoAdminUnits(w http.ResponseWriter, r *http.Request) {
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	items, err := h.svc.ListGeoAdminUnits(r.Context(), r.URL.Query().Get("parent_code"), level)
	writeResultWithRequest(w, r, items, err)
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
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.GetOrganizationByID(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, item, err)
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
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.UpdateOrganization(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteOrganization(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) DeleteParameter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteParameter(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) DeleteLookupCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteLookupCategory(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) DeleteLookupValue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteLookupValue(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) ListCreditInstitutions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListCreditInstitutions(
		r.Context(),
		tenantID,
		r.URL.Query().Get("status"),
		r.URL.Query().Get("q"),
	)
	writeResultWithRequest(w, r, items, err)
}

func (h *PlatformHandler) GetCreditInstitution(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.GetCreditInstitutionByID(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) CreateCreditInstitution(w http.ResponseWriter, r *http.Request) {
	var req domain.CreditInstitution
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" || req.Address == "" || req.Status == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code, name, address and status are required")
		return
	}
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.CreateCreditInstitution(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) UpdateCreditInstitution(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	var req domain.CreditInstitution
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	req.ID = id
	if req.Code == "" || req.Name == "" || req.Address == "" || req.Status == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code, name, address and status are required")
		return
	}
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.UpdateCreditInstitution(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) DeleteCreditInstitution(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteCreditInstitution(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) ListAreas(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListAreas(
		r.Context(),
		tenantID,
		r.URL.Query().Get("status"),
		r.URL.Query().Get("area_type_code"),
		r.URL.Query().Get("parent_id"),
		r.URL.Query().Get("q"),
	)
	writeResultWithRequest(w, r, items, err)
}

func (h *PlatformHandler) GetArea(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.GetAreaByID(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) CreateArea(w http.ResponseWriter, r *http.Request) {
	var req domain.Area
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	if req.Code == "" || req.Name == "" || req.AreaTypeCode == "" || req.Status == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code, name, area_type_code and status are required")
		return
	}
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.CreateArea(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) UpdateArea(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	var req domain.Area
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation.invalid_json", "invalid json")
		return
	}
	req.ID = id
	if req.Code == "" || req.Name == "" || req.AreaTypeCode == "" || req.Status == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "code, name, area_type_code and status are required")
		return
	}
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.UpdateArea(r.Context(), req)
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) DeleteArea(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteArea(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, map[string]bool{"ok": true}, err)
}

func (h *PlatformHandler) ListFileTemplates(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListFileTemplates(r.Context(), tenantID)
	writeResultWithRequest(w, r, items, err)
}

func (h *PlatformHandler) GetFileTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.GetFileTemplateByID(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, item, err)
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
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.CreateFileTemplate(r.Context(), req)
	if err == nil {
		publicID := extractPublicID(item.FileURL)
		if publicID != "" {
			if attachErr := h.media.Attach(r.Context(), []string{publicID}, "file_template", item.Code, r); attachErr != nil {
				slog.Error("failed to attach template file on create", "public_id", publicID, "err", attachErr)
			}
		}
	}
	writeResultWithRequest(w, r, item, err)
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
	if !bindVerifiedTenant(w, r, &req.TenantID) {
		return
	}
	item, err := h.svc.UpdateFileTemplate(r.Context(), req)
	if err == nil {
		publicID := extractPublicID(item.FileURL)
		if publicID != "" {
			if attachErr := h.media.Attach(r.Context(), []string{publicID}, "file_template", item.Code, r); attachErr != nil {
				slog.Error("failed to attach template file on update", "public_id", publicID, "err", attachErr)
			}
		}
	}
	writeResultWithRequest(w, r, item, err)
}

func (h *PlatformHandler) DeleteFileTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation.required", "id is required")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteFileTemplate(r.Context(), tenantID, id)
	writeResultWithRequest(w, r, map[string]bool{"ok": true}, err)
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

func writeResultWithRequest(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if r != nil {
		ardahttp.WriteSuccess(w, r, http.StatusOK, data)
		return
	}
	ardahttp.WriteSuccess(w, nil, http.StatusOK, data)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *ardaerrors.Error
	if errors.As(err, &appErr) {
		status := http.StatusBadRequest
		if appErr.Code == ardaerrors.CodeNotFound {
			status = http.StatusNotFound
		}
		if r != nil {
			ardahttp.WriteProblem(w, r, status, appErr)
			return
		}
		writeErrorCode(w, status, appErr.Code, appErr.Message)
		return
	}
	if r != nil {
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, "common.error.internal", err.Error())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeErrorCode(w, status, "common.error.unknown", message)
}

func writeErrorCode(w http.ResponseWriter, status int, code, message string) {
	ardahttp.WriteProblem(w, nil, status, ardaerrors.New(code, message))
}
