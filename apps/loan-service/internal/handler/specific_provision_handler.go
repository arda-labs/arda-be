package handler

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// SpecificProvisionHandler exposes the LNM.306 per-loan provision flow.
type SpecificProvisionHandler struct {
	svc *service.SpecificProvisionService
}

func NewSpecificProvisionHandler(svc *service.SpecificProvisionService) *SpecificProvisionHandler {
	return &SpecificProvisionHandler{svc: svc}
}

type specificProvisionInput struct {
	AgreementCode string `json:"agreement_code"`
	ProvisionDate string `json:"provision_date"`
}

// ListSpecificProvisions handles GET /api/loan/specific-provisions?status=.
func (h *SpecificProvisionHandler) ListSpecificProvisions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.List(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(1, len(items), len(items), items))
}

// CalculateSpecificProvision handles POST /api/loan/specific-provisions/calculate.
func (h *SpecificProvisionHandler) CalculateSpecificProvision(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req specificProvisionInput
	if !decodeBody(w, r, &req) {
		return
	}
	if req.AgreementCode == "" || req.ProvisionDate == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "agreement_code and provision_date are required")
		return
	}
	preview, err := h.svc.Calculate(r.Context(), tenantID, req.AgreementCode, req.ProvisionDate)
	writeResult(w, r, preview, err)
}

// SubmitSpecificProvision handles POST /api/loan/specific-provisions.
func (h *SpecificProvisionHandler) SubmitSpecificProvision(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req specificProvisionInput
	if !decodeBody(w, r, &req) {
		return
	}
	if req.AgreementCode == "" || req.ProvisionDate == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "agreement_code and provision_date are required")
		return
	}
	item, err := h.svc.Submit(r.Context(), tenantID, actorOf(r), req.AgreementCode, req.ProvisionDate)
	writeResult(w, r, item, err)
}
