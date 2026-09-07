package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	"github.com/arda-labs/arda/apps/mdm-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type InterestRateHandler struct {
	svc *service.InterestRateService
}

func NewInterestRateHandler(svc *service.InterestRateService) *InterestRateHandler {
	return &InterestRateHandler{svc: svc}
}

func (h *InterestRateHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), mdmListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	activeSelected := listReq.Strings("is_active")
	includeInactive := r.URL.Query().Get("include_inactive") == "true" || len(activeSelected) > 0
	items, err := h.svc.List(r.Context(), tenantID, listReq.Q, includeInactive)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items = filterByActive(items, activeSelected, func(item domain.InterestRate) bool {
		return item.IsActive
	})
	applyListSort(items, listReq.Sort, listReq.Order, interestRateSortStringKeys(), interestRateSortTimeKeys())
	listEnvelope(w, r, items, listReq)
}

func (h *InterestRateHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.Get(r.Context(), tenantID, r.PathValue("id"))
	writeResult(w, r, item, err)
}

func (h *InterestRateHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	var item domain.InterestRate
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return
	}
	created, err := h.svc.Create(r.Context(), tenantID, item)
	writeResult(w, r, created, err)
}

func (h *InterestRateHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	var item domain.InterestRate
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return
	}
	updated, err := h.svc.Update(r.Context(), tenantID, r.PathValue("id"), item)
	writeResult(w, r, updated, err)
}

func (h *InterestRateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.Delete(r.Context(), tenantID, r.PathValue("id"))
	writeResult(w, r, json.RawMessage(`{"deleted":true}`), err)
}

func (h *InterestRateHandler) ListTiers(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	tiers, err := h.svc.ListTiers(r.Context(), tenantID, r.PathValue("id"))
	writeResult(w, r, tiers, err)
}

func (h *InterestRateHandler) CreateTier(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	var tier domain.InterestRateTier
	if err := json.NewDecoder(r.Body).Decode(&tier); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return
	}
	created, err := h.svc.CreateTier(r.Context(), tenantID, r.PathValue("id"), tier)
	writeResult(w, r, created, err)
}

func (h *InterestRateHandler) UpdateTier(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	var tier domain.InterestRateTier
	if err := json.NewDecoder(r.Body).Decode(&tier); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return
	}
	updated, err := h.svc.UpdateTier(r.Context(), tenantID, r.PathValue("id"), r.PathValue("tierId"), tier)
	writeResult(w, r, updated, err)
}

func (h *InterestRateHandler) DeleteTier(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteTier(r.Context(), tenantID, r.PathValue("id"), r.PathValue("tierId"))
	writeResult(w, r, json.RawMessage(`{"deleted":true}`), err)
}
