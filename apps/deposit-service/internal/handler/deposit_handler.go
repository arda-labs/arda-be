package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// Settlement surface used by the HTTP handler (interface for testability).
type SettlementService interface {
	Open(ctx context.Context, tenantID string, in *service.OpenSavingsInput) (*repository.Savings, error)
	SubmitSettle(ctx context.Context, tenantID, actor, savingsCode string) (*service.Submission, error)
	ListSavings(ctx context.Context, tenantID string, orgCodes []string, status, q string) ([]repository.Savings, error)
	ListProducts(ctx context.Context, params repository.ListProductsParams) ([]repository.SavingsProduct, error)
	UpsertProduct(ctx context.Context, tenantID, actor string, in *repository.SavingsProduct) (*repository.SavingsProduct, error)
	ListInterbank(ctx context.Context, tenantID string, orgCodes []string, status string) ([]repository.InterbankDeposit, error)
}

// AdditionalDeposit surface used by the HTTP handler.
type AdditionalDepositService interface {
	Submit(ctx context.Context, tenantID, actor, savingsCode string, amountMinor int64, txnDate string) (*service.Submission, error)
}

// ProductRequest surface used by the HTTP handler.
type ProductRequestService interface {
	Submit(ctx context.Context, tenantID, actor string, in service.ProductRequestInput) (*service.Submission, error)
	List(ctx context.Context, tenantID, status string) ([]repository.ProductRequest, error)
}

// IBM surface used by the HTTP handler.
type IBMService interface {
	ListIBMProducts(ctx context.Context, tenantID string, includeInactive bool) ([]repository.IBMProduct, error)
	UpsertIBMProduct(ctx context.Context, tenantID, actor string, in *repository.IBMProduct) (*repository.IBMProduct, error)
	GetIBMDetail(ctx context.Context, tenantID, id string) (*service.IBMDetail, error)
	SubmitPlace(ctx context.Context, tenantID, actor string, in *repository.InterbankDeposit) (*repository.InterbankDeposit, error)
	SubmitMovement(ctx context.Context, tenantID, actor, depositID, kind string, amountMinor int64, movementDate, periodFrom, periodTo, note string) (*repository.IBMMovement, error)
}

// Interest surface used by the HTTP handler (rates + accrual + ops).
type InterestService interface {
	ListInterestRates(ctx context.Context, tenantID, productCode string) ([]repository.InterestRate, error)
	SubmitRate(ctx context.Context, tenantID, actor, requestType string, payload json.RawMessage) (*repository.RateRequest, error)
	SubmitInterest(ctx context.Context, tenantID, actor, savingsCode, opType string, amountMinor int64) (*repository.InterestOp, []repository.InterestOp, error)
	GetSavingsDetail(ctx context.Context, tenantID, code string) (*service.SavingsDetail, error)
	RunDaily(ctx context.Context, tenantID, businessDate string) (int, error)
}

// Report surface used by the HTTP handler (DPM/IBM report reads).
type ReportService interface {
	SavingsStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]repository.SavingsStatementRow, error)
	SavingsTransactions(ctx context.Context, tenantID, fromDate, toDate, txnType string) ([]repository.SavingsTxnRow, error)
	InterbankStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]repository.InterbankStatementRow, error)
	InterbankTransactions(ctx context.Context, tenantID, fromDate, toDate string) ([]repository.InterbankTxnRow, error)
}

// DepositHandler exposes the deposit HTTP surface (P2.1).
type DepositHandler struct {
	svc        SettlementService
	additional AdditionalDepositService
	products   ProductRequestService
	ibm        IBMService
	interest   InterestService
	reports    ReportService
}

func NewDepositHandler(svc SettlementService, additional AdditionalDepositService, products ProductRequestService, ibm IBMService, interest InterestService, reports ReportService) *DepositHandler {
	return &DepositHandler{svc: svc, additional: additional, products: products, ibm: ibm, interest: interest, reports: reports}
}

type orgScope struct {
	ActiveOrgID string
	OrgIDs      []string
}

func orgScopeFrom(r *http.Request) orgScope {
	var ids []string
	if raw := strings.TrimSpace(r.Header.Get("X-User-Org-Ids")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				ids = append(ids, part)
			}
		}
	}
	return orgScope{ActiveOrgID: strings.TrimSpace(r.Header.Get("X-Org-Id")), OrgIDs: ids}
}

func (s orgScope) listFilter() []string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return []string{s.ActiveOrgID}
	}
	return s.OrgIDs
}

func (s orgScope) active() string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return s.ActiveOrgID
	}
	if s.ActiveOrgID == "" && len(s.OrgIDs) == 1 {
		return s.OrgIDs[0]
	}
	return s.ActiveOrgID
}

func (s orgScope) allows(orgID string) bool {
	if orgID == "" {
		return false
	}
	for _, allowed := range s.OrgIDs {
		if allowed == orgID {
			return true
		}
	}
	return false
}

// ListProducts handles GET /api/deposit/products.
func (h *DepositHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	list := ardahttp.ParseListQuery(r.URL.Query())
	params := repository.ListProductsParams{
		TenantID: tenantID,
		Q:        list.Q,
		Sort:     list.Sort,
		Order:    list.Order,
	}
	if active, err := ardahttp.ParseOptionalBool(r.URL.Query(), "is_active"); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	} else {
		params.IsActive = active
	}
	items, err := h.svc.ListProducts(r.Context(), params)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertProduct handles POST/PUT /api/deposit/products.
func (h *DepositHandler) UpsertProduct(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in repository.SavingsProduct
	if !decodeDepositBody(w, r, &in) {
		return
	}
	created, err := h.svc.UpsertProduct(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// OpenSavings handles POST /api/deposit/savings/open.
func (h *DepositHandler) OpenSavings(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in service.OpenSavingsInput
	if !decodeDepositBody(w, r, &in) {
		return
	}
	scope := orgScopeFrom(r)
	in.Actor = r.Header.Get("X-User-Id")
	in.OrgCode = scope.active()
	created, err := h.svc.Open(r.Context(), tenantID, &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// SubmitSettlement handles POST /api/deposit/savings/{code}/settle — creates
// and submits the DPM_SETTLE_V2 maker/checker case (parity DPM.306.01).
func (h *DepositHandler) SubmitSettlement(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	submission, err := h.svc.SubmitSettle(r.Context(), tenantID, r.Header.Get("X-User-Id"), r.PathValue("code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, submission)
}

// SubmitAdditional handles POST /api/deposit/savings/{code}/deposit — creates
// and submits the DPM_ADDITIONAL_V1 maker/checker case (DPM.301.01).
func (h *DepositHandler) SubmitAdditional(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in struct {
		AmountMinor int64  `json:"amount_minor"`
		TxnDate     string `json:"txn_date"`
	}
	if !decodeDepositBody(w, r, &in) {
		return
	}
	submission, err := h.additional.Submit(r.Context(), tenantID, r.Header.Get("X-User-Id"),
		r.PathValue("code"), in.AmountMinor, in.TxnDate)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, submission)
}

// SubmitProductRequest handles POST /api/deposit/product-requests —
// DPM.102/103 staged product payload + maker/checker case.
func (h *DepositHandler) SubmitProductRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in service.ProductRequestInput
	if !decodeDepositBody(w, r, &in) {
		return
	}
	submission, err := h.products.Submit(r.Context(), tenantID, r.Header.Get("X-User-Id"), in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, submission)
}

// ListProductRequests handles GET /api/deposit/product-requests?status=.
func (h *DepositHandler) ListProductRequests(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	items, err := h.products.List(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// ListSavings handles GET /api/deposit/savings.
func (h *DepositHandler) ListSavings(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	scope := orgScopeFrom(r)
	items, err := h.svc.ListSavings(r.Context(), tenantID, scope.listFilter(),
		r.URL.Query().Get("status"), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

func decodeDepositBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := jsonDecodeDeposit(r, target); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return false
	}
	return true
}

// ListInterbankDeposits handles GET /api/deposit/interbank.
func (h *DepositHandler) ListInterbankDeposits(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	scope := orgScopeFrom(r)
	items, err := h.svc.ListInterbank(r.Context(), tenantID, scope.listFilter(), r.URL.Query().Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// CreateInterbankDeposit handles POST /api/deposit/interbank — stages the
// IBM.200.01 placement case.
func (h *DepositHandler) CreateInterbankDeposit(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in repository.InterbankDeposit
	if !decodeDepositBody(w, r, &in) {
		return
	}
	scope := orgScopeFrom(r)
	in.OrgCode = scope.active()
	created, err := h.ibm.SubmitPlace(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// GetInterbankDetail handles GET /api/deposit/interbank/{id}.
func (h *DepositHandler) GetInterbankDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	detail, err := h.ibm.GetIBMDetail(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, detail)
}

// SubmitIBMMovement handles POST /api/deposit/interbank/{id}/movements —
// stages one TOP_UP/INTEREST/EXPECTED/WITHDRAW case.
func (h *DepositHandler) SubmitIBMMovement(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in struct {
		Kind         string `json:"kind"`
		AmountMinor  int64  `json:"amount_minor"`
		MovementDate string `json:"movement_date"`
		PeriodFrom   string `json:"period_from"`
		PeriodTo     string `json:"period_to"`
		Note         string `json:"note"`
	}
	if !decodeDepositBody(w, r, &in) {
		return
	}
	created, err := h.ibm.SubmitMovement(r.Context(), tenantID, r.Header.Get("X-User-Id"),
		r.PathValue("id"), in.Kind, in.AmountMinor, in.MovementDate, in.PeriodFrom, in.PeriodTo, in.Note)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// ListIBMProducts handles GET /api/deposit/ibm-products.
func (h *DepositHandler) ListIBMProducts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	items, err := h.ibm.ListIBMProducts(r.Context(), tenantID, r.URL.Query().Get("include_inactive") == "true")
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertIBMProduct handles POST /api/deposit/ibm-products.
func (h *DepositHandler) UpsertIBMProduct(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in repository.IBMProduct
	if !decodeDepositBody(w, r, &in) {
		return
	}
	created, err := h.ibm.UpsertIBMProduct(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// ListInterestRates handles GET /api/deposit/rates.
func (h *DepositHandler) ListInterestRates(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	items, err := h.interest.ListInterestRates(r.Context(), tenantID, r.URL.Query().Get("product_code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// SubmitRateRequest handles POST /api/deposit/rates — stages a DPM.100/101 case.
func (h *DepositHandler) SubmitRateRequest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in struct {
		RequestType string          `json:"request_type"`
		Payload     json.RawMessage `json:"payload"`
	}
	if !decodeDepositBody(w, r, &in) {
		return
	}
	created, err := h.interest.SubmitRate(r.Context(), tenantID, r.Header.Get("X-User-Id"), in.RequestType, in.Payload)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// GetSavingsDetail handles GET /api/deposit/savings/{code}.
func (h *DepositHandler) GetSavingsDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	detail, err := h.interest.GetSavingsDetail(r.Context(), tenantID, r.PathValue("code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, detail)
}

// SubmitSavingsInterest handles POST /api/deposit/savings/{code}/interest —
// stages a DPM.302/303 op.
func (h *DepositHandler) SubmitSavingsInterest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in struct {
		OpType      string `json:"op_type"`
		AmountMinor int64  `json:"amount_minor"`
	}
	if !decodeDepositBody(w, r, &in) {
		return
	}
	op, _, err := h.interest.SubmitInterest(r.Context(), tenantID, r.Header.Get("X-User-Id"),
		r.PathValue("code"), in.OpType, in.AmountMinor)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, op)
}

// SubmitBatchInterest handles POST /api/deposit/batch-interest — stages the
// DPM.304 batch case over every savings with accrued interest.
func (h *DepositHandler) SubmitBatchInterest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	_, ops, err := h.interest.SubmitInterest(r.Context(), tenantID, r.Header.Get("X-User-Id"), "", "BATCH", 0)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, map[string]any{"items": ops, "total": len(ops)})
}

// RunAccrualDaily handles POST /internal/jobs/deposit-accrual-daily?to_date=.
func (h *DepositHandler) RunAccrualDaily(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	toDate := strings.TrimSpace(r.URL.Query().Get("to_date"))
	if toDate == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "to_date is required"))
		return
	}
	posted, err := h.interest.RunDaily(r.Context(), tenantID, toDate)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"posted": posted})
}

// GetDepositStatement handles GET /api/deposit/reports/deposit-statement.
func (h *DepositHandler) GetDepositStatement(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	q := r.URL.Query()
	items, err := h.reports.SavingsStatement(r.Context(), tenantID, q.Get("from"), q.Get("to"), q.Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// GetDepositTransactions handles GET /api/deposit/reports/deposit-transactions.
func (h *DepositHandler) GetDepositTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	q := r.URL.Query()
	items, err := h.reports.SavingsTransactions(r.Context(), tenantID, q.Get("from"), q.Get("to"), q.Get("txn_type"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// GetInterbankStatement handles GET /api/deposit/reports/interbank-statement.
func (h *DepositHandler) GetInterbankStatement(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	q := r.URL.Query()
	items, err := h.reports.InterbankStatement(r.Context(), tenantID, q.Get("from"), q.Get("to"), q.Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// GetInterbankTransactions handles GET /api/deposit/reports/interbank-transactions.
func (h *DepositHandler) GetInterbankTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	q := r.URL.Query()
	items, err := h.reports.InterbankTransactions(r.Context(), tenantID, q.Get("from"), q.Get("to"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}
