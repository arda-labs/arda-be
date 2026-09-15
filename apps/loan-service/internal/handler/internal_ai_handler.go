package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// aiMaxQueryLen / aiMaxIdentifierLen mirror the q and identifier schemas in
// contracts/ai-internal/loan-v1.json (both capped at 128 characters).
const (
	aiMaxQueryLen      = 128
	aiMaxIdentifierLen = 128
)

// aiLoanListSpec is the AI tool list contract: per_page 1..20 with default 10.
// sort/view/all are intentionally absent — the assistant can never pull an
// unbounded page, and the list stays on the SQL-paged contract endpoints.
var aiLoanListSpec = ardahttp.ListSpec{
	DefaultPerPage: 10,
	MaxPerPage:     20,
}

// aiLoanSource is the slice of LoanService the AI surface needs. The
// interface form keeps the handler testable without a database while the
// concrete service satisfies it as-is.
type aiLoanSource interface {
	ListContractsPaged(ctx context.Context, tenantID, status, q, sort, order string, page, perPage int) ([]domain.Contract, int, error)
	GetContract(ctx context.Context, tenantID, id string) (domain.Contract, error)
	ListRepayPlans(ctx context.Context, tenantID, contractCode, agreementCode string) ([]domain.RepayPlan, error)
}

// InternalAIHandler serves the /internal/ai/* surface consumed by ai-service.
// The signed caller assertion is verified by the router's internalAIService
// middleware; the delegated subject (X-Tenant-Id) is re-validated here and
// passed to the service layer, so a tenant can never read another tenant's
// credit data.
type InternalAIHandler struct {
	loans aiLoanSource
}

func NewInternalAIHandler(loans aiLoanSource) *InternalAIHandler {
	return &InternalAIHandler{loans: loans}
}

// InternalAIListContracts serves GET /internal/ai/contracts for ai-service.
// It reuses the SQL-paged contract list (q narrows contract_no/customer_code/
// contract_code) and returns the redacted summary shape only.
func (h *InternalAIHandler) InternalAIListContracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), aiLoanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, total, err := h.loans.ListContractsPaged(r.Context(), tenantID,
		aiStatus(r), aiQuery(r), "", "", listReq.Page, listReq.PerPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK,
		ardahttp.NewListResponse(listReq.Page, listReq.PerPage, total, toAIContractSummaries(items)))
}

// InternalAIGetContract serves GET /internal/ai/contracts/{id} for ai-service.
func (h *InternalAIHandler) InternalAIGetContract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > aiMaxIdentifierLen {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "contract id must be 1-128 characters")
		return
	}
	item, err := h.loans.GetContract(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, toAIContractDetail(item))
}

// InternalAIListRepayPlans serves GET /internal/ai/repay-plans for ai-service.
// One schedule key (contract_code or agreement_code) is required: without it
// the underlying list would return every plan in the tenant, which the
// assistant must never be able to pull.
func (h *InternalAIHandler) InternalAIListRepayPlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	contractCode := strings.TrimSpace(r.URL.Query().Get("contract_code"))
	agreementCode := strings.TrimSpace(r.URL.Query().Get("agreement_code"))
	if contractCode == "" && agreementCode == "" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeRequired, "contract_code or agreement_code is required")
		return
	}
	if len(contractCode) > aiMaxIdentifierLen || len(agreementCode) > aiMaxIdentifierLen {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "contract_code and agreement_code must be at most 128 characters")
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), aiLoanListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.loans.ListRepayPlans(r.Context(), tenantID, contractCode, agreementCode)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	paged, total, page, perPage := ardahttp.PageSlice(items, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK,
		ardahttp.NewListResponse(page, perPage, total, toAIRepayPlans(paged)))
}

// aiQuery trims the free-text search and clamps it to aiMaxQueryLen
// (rune-safe, the query can carry Vietnamese text).
func aiQuery(r *http.Request) string {
	return aiClamp(r.URL.Query().Get("q"), aiMaxQueryLen)
}

// aiStatus normalizes the contract status filter; contract statuses are
// stored uppercase, so the assistant may pass either casing.
func aiStatus(r *http.Request) string {
	return strings.ToUpper(aiClamp(r.URL.Query().Get("status"), 32))
}

// aiClamp trims a free-text parameter and caps it at max runes.
func aiClamp(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

// aiMethodNotAllowed guards the AI handlers when they are invoked directly
// (the routes themselves are registered GET-only).
func aiMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeErrorCode(w, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
}

// aiLoanContractSummary is the redacted contract-list shape exposed to the AI
// SDK. tenant_id, employee_code, org/workflow linkage, audit fields and the
// remaining credit-classification codes are dropped here; the response
// allowlist in contracts/ai-internal/loan-v1.json drops them again as defense
// in depth.
type aiLoanContractSummary struct {
	ID               string  `json:"id"`
	ContractCode     string  `json:"contract_code"`
	ContractNo       string  `json:"contract_no"`
	CustomerCode     string  `json:"customer_code"`
	ContractTypeCode string  `json:"contract_type_code"`
	ProductCode      string  `json:"product_code"`
	ContractDate     string  `json:"contract_date"`
	MaturityDate     string  `json:"maturity_date"`
	LoanAmtMinor     int64   `json:"loan_amt_minor"`
	InterestRate     float64 `json:"interest_rate"`
	Status           string  `json:"status"`
}

func toAIContractSummaries(items []domain.Contract) []aiLoanContractSummary {
	redacted := make([]aiLoanContractSummary, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiLoanContractSummary{
			ID:               item.ID,
			ContractCode:     item.ContractCode,
			ContractNo:       item.ContractNo,
			CustomerCode:     item.CustomerCode,
			ContractTypeCode: item.ContractTypeCode,
			ProductCode:      item.ProductCode,
			ContractDate:     item.ContractDate,
			MaturityDate:     item.MaturityDate,
			LoanAmtMinor:     item.LoanAmt,
			InterestRate:     item.InterestRate,
			Status:           item.Status,
		})
	}
	return redacted
}

// aiLoanContractDetail is the redacted contract-detail shape. It adds the
// schedule/term fields the summary omits while still dropping tenant_id,
// employee_code, org_code, workflow linkage and audit fields.
type aiLoanContractDetail struct {
	ID                   string  `json:"id"`
	ContractCode         string  `json:"contract_code"`
	ContractNo           string  `json:"contract_no"`
	CustomerCode         string  `json:"customer_code"`
	ContractTypeCode     string  `json:"contract_type_code"`
	ProductCode          string  `json:"product_code"`
	ContractDate         string  `json:"contract_date"`
	LoanTerm             int     `json:"loan_term"`
	TermUnit             string  `json:"term_unit"`
	MaturityDate         string  `json:"maturity_date"`
	LoanAmtMinor         int64   `json:"loan_amt_minor"`
	InterestRate         float64 `json:"interest_rate"`
	InterestRateType     string  `json:"interest_rate_type"`
	InterestPaymentFreq  string  `json:"interest_payment_freq"`
	PrincipalPaymentFreq string  `json:"principal_payment_freq"`
	Status               string  `json:"status"`
}

func toAIContractDetail(item domain.Contract) aiLoanContractDetail {
	return aiLoanContractDetail{
		ID:                   item.ID,
		ContractCode:         item.ContractCode,
		ContractNo:           item.ContractNo,
		CustomerCode:         item.CustomerCode,
		ContractTypeCode:     item.ContractTypeCode,
		ProductCode:          item.ProductCode,
		ContractDate:         item.ContractDate,
		LoanTerm:             item.LoanTerm,
		TermUnit:             item.TermUnit,
		MaturityDate:         item.MaturityDate,
		LoanAmtMinor:         item.LoanAmt,
		InterestRate:         item.InterestRate,
		InterestRateType:     item.InterestRateType,
		InterestPaymentFreq:  item.InterestPaymentFreq,
		PrincipalPaymentFreq: item.PrincipalPaymentFreq,
		Status:               item.Status,
	}
}

// aiRepayPlan is the redacted schedule row exposed to the AI SDK. tenant_id,
// row id and audit timestamps are dropped; planned and collected amounts plus
// the term window are kept (they answer "what is the repayment schedule").
type aiRepayPlan struct {
	ContractCode          string  `json:"contract_code"`
	AgreementCode         string  `json:"agreement_code"`
	PlanNo                int     `json:"plan_no"`
	TermNo                int     `json:"term_no"`
	FromDate              string  `json:"from_date"`
	ToDate                string  `json:"to_date"`
	InterestRate          float64 `json:"interest_rate"`
	PlanPrincipalAmtMinor int64   `json:"plan_principal_amt_minor"`
	PlanInterestAmtMinor  int64   `json:"plan_interest_amt_minor"`
	ColnPrincipalAmtMinor int64   `json:"coln_principal_amt_minor"`
	ColnInterestAmtMinor  int64   `json:"coln_interest_amt_minor"`
	IsActive              bool    `json:"is_active"`
}

func toAIRepayPlans(items []domain.RepayPlan) []aiRepayPlan {
	redacted := make([]aiRepayPlan, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiRepayPlan{
			ContractCode:          item.ContractCode,
			AgreementCode:         item.AgreementCode,
			PlanNo:                item.PlanNo,
			TermNo:                item.TermNo,
			FromDate:              item.FromDate,
			ToDate:                item.ToDate,
			InterestRate:          item.InterestRate,
			PlanPrincipalAmtMinor: item.PlanPrincipalAmt,
			PlanInterestAmtMinor:  item.PlanInterestAmt,
			ColnPrincipalAmtMinor: item.ColnPrincipalAmt,
			ColnInterestAmtMinor:  item.ColnInterestAmt,
			IsActive:              item.IsActive,
		})
	}
	return redacted
}
