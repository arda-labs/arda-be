package handler

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
)

// DisbursementHandler exposes the LNM.300.02 drawdown flow over HTTP.
type DisbursementHandler struct {
	svc *service.DisbursementService
}

func NewDisbursementHandler(svc *service.DisbursementService) *DisbursementHandler {
	return &DisbursementHandler{svc: svc}
}

// ListDisbursements handles GET /api/loan/disbursements.
func (h *DisbursementHandler) ListDisbursements(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.List(r.Context(), tenantID, orgScopeFromRequest(r).ListFilter(), r.URL.Query().Get("status"), r.URL.Query().Get("contract_code"))
	writeResult(w, r, items, err)
}

// CreateDisbursement handles POST /api/loan/disbursements.
func (h *DisbursementHandler) CreateDisbursement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.Disbursement
	if !decodeBody(w, r, &req) {
		return
	}
	req.OrgCode = orgScopeFromRequest(r).ActiveOrg()
	created, err := h.svc.Create(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, created, err)
}

// SubmitDisbursement handles POST /api/loan/disbursements/{id}/submit.
func (h *DisbursementHandler) SubmitDisbursement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.Submit(r.Context(), tenantID, actorOf(r), r.PathValue("id"))
	writeResult(w, r, item, err)
}
