package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

const (
	aiMaxQueryLength  = 128
	aiMaxLookupCode   = 64
	aiOrgDefaultLimit = 20
	aiOrgMaxLimit     = 20
	aiOrgDefaultPage  = 1
)

// aiOrganization is the redacted organization shape exposed to the AI SDK.
// The tenant linkage is dropped here; the response allowlist in
// contracts/ai-internal/platform-v1.json drops it again as defense in depth.
type aiOrganization struct {
	ID         string  `json:"id"`
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	ParentID   *string `json:"parentId,omitempty"`
	ParentName *string `json:"parentName,omitempty"`
	IsActive   bool    `json:"isActive"`
}

func toAIOrganizations(items []domain.Organization) []aiOrganization {
	redacted := make([]aiOrganization, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiOrganization{
			ID:         item.ID,
			Code:       item.Code,
			Name:       item.Name,
			ParentID:   item.ParentID,
			ParentName: item.ParentName,
			IsActive:   item.IsActive,
		})
	}
	return redacted
}

// aiParameter is the redacted parameter shape exposed to the AI SDK. Rows
// marked is_secret are filtered out entirely — Parameter.value never reaches
// the AI surface — and tenant_id / scope_id linkage is dropped.
type aiParameter struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Value       string  `json:"value"`
	ValueType   string  `json:"valueType"`
	ScopeType   string  `json:"scopeType"`
	Description *string `json:"description,omitempty"`
}

func toAIParameters(items []domain.Parameter) []aiParameter {
	redacted := make([]aiParameter, 0, len(items))
	for _, item := range items {
		if item.IsSecret {
			continue
		}
		redacted = append(redacted, aiParameter{
			ID:          item.ID,
			Key:         item.Key,
			Value:       item.Value,
			ValueType:   item.ValueType,
			ScopeType:   item.ScopeType,
			Description: item.Description,
		})
	}
	return redacted
}

// aiLookupValue is the redacted lookup-value shape exposed to the AI SDK.
// Internal linkage fields (category_id, metadata) are dropped.
type aiLookupValue struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	IsActive  bool   `json:"isActive"`
}

func toAILookupValues(items []domain.LookupValue) []aiLookupValue {
	redacted := make([]aiLookupValue, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiLookupValue{
			ID:        item.ID,
			Code:      item.Code,
			Name:      item.Name,
			SortOrder: item.SortOrder,
			IsActive:  item.IsActive,
		})
	}
	return redacted
}

// InternalAIListOrganizations serves GET /internal/ai/organizations for
// ai-service. The signed caller assertion is verified by the router's
// internalAIService middleware; the tenant is taken from the delegated
// X-Tenant-Id header, never from tool arguments.
func (h *PlatformHandler) InternalAIListOrganizations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorCode(w, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if len(q) > aiMaxQueryLength {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "q must be at most 128 characters")
		return
	}
	page := aiOrgDefaultPage
	if raw := strings.TrimSpace(query.Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < aiOrgDefaultPage {
			writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "page must be a positive integer")
			return
		}
		page = parsed
	}
	perPage := aiOrgDefaultLimit
	if raw := strings.TrimSpace(query.Get("per_page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > aiOrgMaxLimit {
			writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "per_page must be an integer between 1 and 20")
			return
		}
		perPage = parsed
	}
	items, total, err := h.svc.ListOrganizations(r.Context(), repository.ListOrganizationsParams{
		TenantID: tenantID,
		Page:     page,
		PerPage:  perPage,
		Offset:   (page - 1) * perPage,
		Query:    q,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, toAIOrganizations(items)))
}

// InternalAIListParameters serves GET /internal/ai/parameters for ai-service.
// Rows marked is_secret are filtered out before any response is written, so
// Parameter.value of secret parameters is never exposed.
func (h *PlatformHandler) InternalAIListParameters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorCode(w, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListParameters(r.Context(), tenantID, strings.TrimSpace(r.URL.Query().Get("scope_type")), "")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"items": toAIParameters(items)})
}

// InternalAILookupValues serves GET /internal/ai/lookups/{lookupCode}/values
// for ai-service.
func (h *PlatformHandler) InternalAILookupValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorCode(w, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	lookupCode := r.PathValue("lookupCode")
	if lookupCode == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "lookup code is required")
		return
	}
	if len(lookupCode) > aiMaxLookupCode {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "lookup code must be at most 64 characters")
		return
	}
	items, err := h.svc.ListLookupValues(r.Context(), tenantID, lookupCode)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"items": toAILookupValues(items)})
}

// InternalAICalendarStatus serves GET /internal/ai/calendar/status for
// ai-service. It reuses the same system-date logic as the public
// /api/platform/calendar/status route.
func (h *CalendarHandler) InternalAICalendarStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorCode(w, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := requiredTenantID(w, r); !ok {
		return
	}
	branchCode := strings.TrimSpace(r.URL.Query().Get("branchCode"))
	if branchCode == "" {
		branchCode = "HEAD_OFFICE"
	}
	sd, err := h.service.GetSystemDate(r.Context(), branchCode)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, "common.error.internal", err.Error())
		return
	}
	if sd == nil {
		writeErrorCode(w, http.StatusNotFound, "calendar.error.not_found", "system date config not found")
		return
	}
	writeResultWithRequest(w, r, sd, nil)
}
