package handler

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// GeneralProvisionHandler exposes the LNM.307.01 per-org provision flow.
type GeneralProvisionHandler struct {
	svc *service.GeneralProvisionService
}

func NewGeneralProvisionHandler(svc *service.GeneralProvisionService) *GeneralProvisionHandler {
	return &GeneralProvisionHandler{svc: svc}
}

type generalProvisionInput struct {
	OrgCode       string `json:"org_code"`
	ProvisionDate string `json:"provision_date"`
}

// ListGeneralProvisions handles GET /api/loan/general-provisions?org=.
func (h *GeneralProvisionHandler) ListGeneralProvisions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.List(r.Context(), tenantID, r.URL.Query().Get("org"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(1, len(items), len(items), items))
}

// CalculateGeneralProvision handles POST /api/loan/general-provisions/calculate
// — pure preview, no persistence.
func (h *GeneralProvisionHandler) CalculateGeneralProvision(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req generalProvisionInput
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ProvisionDate == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "provision_date is required")
		return
	}
	preview, err := h.svc.Calculate(r.Context(), tenantID, req.OrgCode, req.ProvisionDate)
	writeResult(w, r, preview, err)
}

// SubmitGeneralProvision handles POST /api/loan/general-provisions — calculates,
// stores the period SUBMITTED and starts the approval case.
func (h *GeneralProvisionHandler) SubmitGeneralProvision(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req generalProvisionInput
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ProvisionDate == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "provision_date is required")
		return
	}
	item, err := h.svc.Submit(r.Context(), tenantID, actorOf(r), req.OrgCode, req.ProvisionDate)
	writeResult(w, r, item, err)
}
