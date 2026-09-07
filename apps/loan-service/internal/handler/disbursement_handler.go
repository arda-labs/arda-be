package handler

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// DisbursementHandler exposes the LNM.300.02 drawdown flow over HTTP.
type DisbursementHandler struct {
	svc *service.DisbursementService
}

func NewDisbursementHandler(svc *service.DisbursementService) *DisbursementHandler {
	return &DisbursementHandler{svc: svc}
}

// disbursementListSpec is the ParseListRequest contract for the disbursement
// ledger: q (agreement/contract code ILIKE), status + contract_code filters
// and a SQL sort whitelist. SQL paging — no full-table slice.
var disbursementListSpec = ardahttp.ListSpec{
	DefaultPerPage: 20,
	MaxPerPage:     ardahttp.MaxPerPage,
	SortFields:     []string{"agreement_code", "contract_code", "disburse_date", "created_at"},
}

// ListDisbursements handles GET /api/loan/disbursements.
func (h *DisbursementHandler) ListDisbursements(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), disbursementListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, total, err := h.svc.List(r.Context(), tenantID, orgScopeFromRequest(r).ListFilter(),
		r.URL.Query().Get("status"), r.URL.Query().Get("contract_code"),
		listReq.Q, listReq.Sort, listReq.Order, listReq.Page, listReq.PerPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listReq.Page, listReq.PerPage, total, items))
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
