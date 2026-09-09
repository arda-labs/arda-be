package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func requireTenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "verified tenant scope is required")
		return "", false
	}
	return tenantID, true
}

func writeErrorCode(w http.ResponseWriter, status int, code, message string) {
	ardahttp.WriteProblem(w, nil, status, ardaerrors.New(code, message))
}

func writeResult(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, data)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *ardaerrors.Error
	if errors.As(err, &appErr) {
		status := http.StatusBadRequest
		switch appErr.Code {
		case ardaerrors.CodeNotFound:
			status = http.StatusNotFound
		case ardaerrors.CodeConflict:
			status = http.StatusConflict
		case ardaerrors.CodeBadGateway:
			status = http.StatusBadGateway
		}
		ardahttp.WriteProblem(w, r, status, appErr)
		return
	}
	ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return false
	}
	return true
}

func actorOf(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-User-Id"))
}

type LoanHandler struct {
	svc *service.LoanService
	adj *service.AdjustmentService
}

func NewLoanHandler(svc *service.LoanService, adj *service.AdjustmentService) *LoanHandler {
	return &LoanHandler{svc: svc, adj: adj}
}

// loanListSpec is the shared list contract for loan list endpoints:
// sort whitelist and allow-all (client tables page/filter locally).
var loanListSpec = ardahttp.ListSpec{
	DefaultPerPage: 20,
	MaxPerPage:     ardahttp.MaxPerPage,
	SortFields:     []string{"code", "name", "amount", "created_at"},
	AllowAll:       true,
}

// contractListSpec is the public list contract for GET /api/loan/contracts
// (normalized server list, iteration 12): SQL paging, q ILIKE
// contract_no/customer_code/contract_code and a sort whitelist kept in sync
// with the FE list definition (created_at today; contract_no and
// loan_amt_minor are the reserved BE keys). AllowAll keeps the legacy
// "everything up to 500" behavior reachable for the local-table screens.
var contractListSpec = ardahttp.ListSpec{
	DefaultPerPage: 50,
	MaxPerPage:     200,
	SortFields:     []string{"created_at", "contract_no", "loan_amt_minor"},
	AllowAll:       true,
}

// productListSpec narrows the list contract to the product catalog: q is
// applied in SQL (code + name ILIKE), is_active accepts a true/false CSV
// filter, and the sort whitelist matches the ORDER BY switch in the repo.
var productListSpec = ardahttp.ListSpec{
	DefaultPerPage: 20,
	MaxPerPage:     ardahttp.MaxPerPage,
	SortFields:     []string{"code", "name", "created_at"},
	AllowAll:       true,
	Filters: map[string]ardahttp.QueryFilterSpec{
		"is_active": ardahttp.CSVFilter(2, "true", "false"),
	},
}

// listEnvelope paginates the fetched slice per the parsed list request and
// writes the canonical ListResponse envelope.
func listEnvelope[T any](w http.ResponseWriter, r *http.Request, items []T, listReq ardahttp.ListRequest) {
	paged, page, perPage, total := ardahttp.PageSlice(items, listReq.ListQuery)
	perPageOut := perPage
	if listReq.All {
		perPageOut = len(items)
		if perPageOut == 0 {
			perPageOut = total
		}
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPageOut, total, paged))
}

func (h *LoanHandler) ListContracts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), contractListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	perPage := listReq.PerPage
	if listReq.All {
		// Legacy unpaged shape: the old endpoint returned LIMIT 500 newest
		// first — keep that ceiling for all=true clients.
		perPage = ardahttp.MaxUnpaginated
	}
	items, total, err := h.svc.ListContractsPaged(r.Context(), tenantID,
		r.URL.Query().Get("status"), listReq.Q, listReq.Sort, listReq.Order, listReq.Page, perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listReq.Page, perPage, total, items))
}

func (h *LoanHandler) GetContract(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.GetContract(r.Context(), tenantID, r.PathValue("id"))
	writeResult(w, r, item, err)
}

func (h *LoanHandler) CreateContract(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.Contract
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.CreateContract(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

func (h *LoanHandler) SubmitContract(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.SubmitContract(r.Context(), tenantID, actorOf(r), r.PathValue("id"))
	writeResult(w, r, item, err)
}

func (h *LoanHandler) ListAgreements(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.svc.ListAgreements(r.Context(), tenantID, r.URL.Query().Get("contract_code"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) CreateAgreement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.Agreement
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.CreateAgreement(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

func (h *LoanHandler) ListRepayPlans(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	items, err := h.svc.ListRepayPlans(r.Context(), tenantID, q.Get("contract_code"), q.Get("agreement_code"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	// Schedule rows are small and always consumed whole — unpaged envelope.
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

func (h *LoanHandler) ListMortgages(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.svc.ListMortgages(r.Context(), tenantID, r.URL.Query().Get("q"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) CreateMortgage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.Mortgage
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.CreateMortgage(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

func (h *LoanHandler) ListCollaterals(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	q := r.URL.Query()
	items, err := h.svc.ListCollaterals(r.Context(), tenantID, q.Get("mortgage_code"), q.Get("q"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) CreateCollateral(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.Collateral
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.CreateCollateral(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

func (h *LoanHandler) ListContractCollaterals(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.svc.ListContractCollaterals(r.Context(), tenantID, r.URL.Query().Get("contract_code"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) AttachContractCollateral(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.ContractCollateral
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.AttachContractCollateral(r.Context(), tenantID, &req)
	writeResult(w, r, item, err)
}

// Adjustment endpoints — one uniform set for every registered kind.

func (h *LoanHandler) ListAdjustments(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	q := r.URL.Query()
	items, err := h.adj.List(r.Context(), r.PathValue("kind"), tenantID, q.Get("contract_code"), q.Get("status"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) GetAdjustment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.adj.Get(r.Context(), r.PathValue("kind"), tenantID, r.PathValue("id"))
	writeResult(w, r, item, err)
}

func (h *LoanHandler) CreateAdjustment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.Adjustment
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.adj.Create(r.Context(), r.PathValue("kind"), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

func (h *LoanHandler) SubmitAdjustment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.adj.Submit(r.Context(), r.PathValue("kind"), tenantID, actorOf(r), r.PathValue("id"))
	writeResult(w, r, item, err)
}

// ── Products ──

func (h *LoanHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), productListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	includeInactive := r.URL.Query().Get("include_inactive") == "true"
	// "true,false" (both selected) means no active-state filter.
	isActive := strings.Join(listReq.Strings("is_active"), ",")
	items, err := h.svc.ListProducts(r.Context(), tenantID, includeInactive, isActive, listReq.Q, listReq.Sort, listReq.Order)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) UpsertProduct(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.LoanProduct
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.UpsertProduct(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

// ── VFU (ủy thác) ──

func (h *LoanHandler) ListVfuParties(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.svc.ListVfuParties(r.Context(), tenantID, r.URL.Query().Get("q"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) CreateVfuParty(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.VfuParty
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.CreateVfuParty(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

func (h *LoanHandler) ListVfuMandates(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.svc.ListVfuMandates(r.Context(), tenantID, r.URL.Query().Get("q"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) CreateVfuMandate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.VfuMandate
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.CreateVfuMandate(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

func (h *LoanHandler) ListVfuPlans(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), loanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.svc.ListVfuPlans(r.Context(), tenantID, r.URL.Query().Get("mandate_code"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	listEnvelope(w, r, items, listReq)
}

func (h *LoanHandler) CreateVfuPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.VfuPlan
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.CreateVfuPlan(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, item, err)
}

// GetDossier handles GET /api/loan/contracts/{id}/dossier — the composite
// dossier view (contract + agreements + plans + movements + TSBĐ + cases).
func (h *LoanHandler) GetDossier(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	dossier, err := h.svc.Dossier(r.Context(), tenantID, r.PathValue("id"))
	writeResult(w, r, dossier, err)
}
