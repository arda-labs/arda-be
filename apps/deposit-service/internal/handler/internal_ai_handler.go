package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// aiMaxQueryLen / aiMaxIdentifierLen mirror the q, code and product_code
// schemas in contracts/ai-internal/deposit-v1.json; aiMaxTxnRows bounds the
// transaction preview returned by the savings-detail tool.
const (
	aiMaxQueryLen      = 128
	aiMaxIdentifierLen = 128
	aiMaxTxnRows       = 20
)

// aiDepositListSpec is the AI tool list contract: per_page 1..20 with default
// 10. sort/view/all stay out of the assistant surface so a page can never grow
// unbounded.
var aiDepositListSpec = ardahttp.ListSpec{
	DefaultPerPage: 10,
	MaxPerPage:     20,
}

// aiSavingsSource / aiSavingsDetailSource / aiRateSource are the slices of
// the settlement + interest services the AI surface needs. The interface form
// keeps the handler testable without a database while the concrete services
// satisfy them as-is (SettlementService → list, InterestService → detail and
// rates).
type aiSavingsSource interface {
	ListSavings(ctx context.Context, tenantID string, orgCodes []string, status, q string) ([]repository.Savings, error)
}

type aiSavingsDetailSource interface {
	GetSavingsDetail(ctx context.Context, tenantID, code string) (*service.SavingsDetail, error)
}

type aiRateSource interface {
	ListInterestRates(ctx context.Context, tenantID, productCode string) ([]repository.InterestRate, error)
}

// InternalAIHandler serves the /internal/ai/* surface consumed by ai-service.
// The signed caller assertion is verified by the router's internalAIService
// middleware; the delegated subject (X-Tenant-Id, X-Org-Id, X-User-Org-Ids)
// is re-validated here and passed to the service layer, so a tenant can never
// read another tenant's deposit data.
type InternalAIHandler struct {
	savings aiSavingsSource
	detail  aiSavingsDetailSource
	rates   aiRateSource
}

func NewInternalAIHandler(savings aiSavingsSource, detail aiSavingsDetailSource, rates aiRateSource) *InternalAIHandler {
	return &InternalAIHandler{savings: savings, detail: detail, rates: rates}
}

// InternalAIListSavings serves GET /internal/ai/savings for ai-service. It
// reuses the savings list (q narrows savings_code/customer_code) under the
// delegated org scope and returns the redacted savings shape only.
func (h *InternalAIHandler) InternalAIListSavings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := aiTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), aiDepositListSpec)
	if err != nil {
		aiBadRequest(w, r, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.savings.ListSavings(r.Context(), tenantID, orgScopeFrom(r).listFilter(), aiStatus(r), aiQuery(r))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	res := ardahttp.PageSlice(items, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK,
		ardahttp.NewListResponse(res.Page, res.PerPage, res.Total, toAISavings(res.Items)))
}

// InternalAIGetSavingsDetail serves GET /internal/ai/savings/{code} for
// ai-service. The tool returns the redacted savings row plus a bounded,
// redacted transaction preview (newest first): transaction_count reports how
// many rows exist in total.
func (h *InternalAIHandler) InternalAIGetSavingsDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := aiTenantID(w, r)
	if !ok {
		return
	}
	code := strings.TrimSpace(r.PathValue("code"))
	if code == "" || len(code) > aiMaxIdentifierLen {
		aiBadRequest(w, r, ardaerrors.CodeInvalidInput, "savings code must be 1-128 characters")
		return
	}
	detail, err := h.detail.GetSavingsDetail(r.Context(), tenantID, code)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	if detail == nil || detail.Savings == nil {
		aiNotFound(w, r, "savings not found")
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, toAISavingsDetail(detail))
}

// InternalAIListInterestRates serves GET /internal/ai/interest-rates for
// ai-service: active deposit rate tiers, optionally narrowed to one product.
func (h *InternalAIHandler) InternalAIListInterestRates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := aiTenantID(w, r)
	if !ok {
		return
	}
	productCode := strings.TrimSpace(r.URL.Query().Get("product_code"))
	if len(productCode) > aiMaxIdentifierLen {
		aiBadRequest(w, r, ardaerrors.CodeInvalidInput, "product_code must be at most 128 characters")
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), aiDepositListSpec)
	if err != nil {
		aiBadRequest(w, r, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.rates.ListInterestRates(r.Context(), tenantID, productCode)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	res := ardahttp.PageSlice(items, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK,
		ardahttp.NewListResponse(res.Page, res.PerPage, res.Total, toAIDepositRates(res.Items)))
}

// aiTenantID reads the delegated tenant off the signed request headers. The
// AI surface never takes a tenant from tool arguments.
func aiTenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return "", false
	}
	return tenantID, true
}

func aiBadRequest(w http.ResponseWriter, r *http.Request, code, message string) {
	ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(code, message))
}

func aiNotFound(w http.ResponseWriter, r *http.Request, message string) {
	ardahttp.WriteProblem(w, r, http.StatusNotFound, ardaerrors.New(ardaerrors.CodeNotFound, message))
}

// aiMethodNotAllowed guards the AI handlers when they are invoked directly
// (the routes themselves are registered GET-only).
func aiMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}

// aiQuery trims the free-text search and clamps it to aiMaxQueryLen
// (rune-safe, the query can carry Vietnamese text).
func aiQuery(r *http.Request) string {
	return aiClamp(r.URL.Query().Get("q"), aiMaxQueryLen)
}

// aiStatus normalizes the savings status filter to the stored uppercase form.
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

// aiSavings is the redacted savings shape exposed to the AI SDK. tenant_id,
// the internal row id, org_code, workflow/journal linkage and audit fields are
// dropped here; the response allowlist in
// contracts/ai-internal/deposit-v1.json drops them again as defense in depth.
// customer_code is a bank reference code, not raw PII.
type aiSavings struct {
	SavingsCode    string `json:"savings_code"`
	CustomerCode   string `json:"customer_code"`
	ProductCode    string `json:"product_code"`
	OpenDate       string `json:"open_date"`
	MaturityDate   string `json:"maturity_date"`
	PrincipalMinor int64  `json:"principal_minor"`
	AccruedMinor   int64  `json:"accrued_minor"`
	CurrencyCode   string `json:"currency_code"`
	Status         string `json:"status"`
}

func toAISavings(items []repository.Savings) []aiSavings {
	redacted := make([]aiSavings, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiSavings{
			SavingsCode:    item.SavingsCode,
			CustomerCode:   item.CustomerCode,
			ProductCode:    item.ProductCode,
			OpenDate:       item.OpenDate,
			MaturityDate:   item.MaturityDate,
			PrincipalMinor: item.PrincipalMinor,
			AccruedMinor:   item.AccruedMinor,
			CurrencyCode:   item.CurrencyCode,
			Status:         item.Status,
		})
	}
	return redacted
}

// aiSavingsTxn is the redacted transaction preview row; internal ids, journal
// and workflow linkage and audit fields are dropped.
type aiSavingsTxn struct {
	TxnType      string `json:"txn_type"`
	AmountMinor  int64  `json:"amount_minor"`
	CurrencyCode string `json:"currency_code"`
	TxnDate      string `json:"txn_date"`
	Status       string `json:"status"`
}

func toAISavingsTxns(items []repository.DepositTxn) []aiSavingsTxn {
	redacted := make([]aiSavingsTxn, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiSavingsTxn{
			TxnType:      item.TxnType,
			AmountMinor:  item.AmountMinor,
			CurrencyCode: item.CurrencyCode,
			TxnDate:      item.TxnDate,
			Status:       item.Status,
		})
	}
	return redacted
}

// aiSavingsDetail is the savings-detail payload: the redacted savings row, a
// bounded transaction preview (aiMaxTxnRows newest rows) and the full
// transaction count.
type aiSavingsDetail struct {
	Savings          aiSavings      `json:"savings"`
	Transactions     []aiSavingsTxn `json:"transactions"`
	TransactionCount int            `json:"transaction_count"`
}

func toAISavingsDetail(detail *service.SavingsDetail) aiSavingsDetail {
	txns := detail.Txns
	if len(txns) > aiMaxTxnRows {
		txns = txns[:aiMaxTxnRows]
	}
	return aiSavingsDetail{
		Savings:          toAISavings([]repository.Savings{*detail.Savings})[0],
		Transactions:     toAISavingsTxns(txns),
		TransactionCount: len(detail.Txns),
	}
}

// aiDepositRate is the redacted interest-rate tier shape. tenant_id, row id
// and audit fields are dropped; rate/product/term/effective date are kept.
type aiDepositRate struct {
	ProductCode   string  `json:"product_code"`
	TermMonths    int     `json:"term_months"`
	Method        string  `json:"method"`
	Denominator   int     `json:"denominator"`
	Rate          float64 `json:"rate"`
	EffectiveFrom string  `json:"effective_from"`
	IsActive      bool    `json:"is_active"`
}

func toAIDepositRates(items []repository.InterestRate) []aiDepositRate {
	redacted := make([]aiDepositRate, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiDepositRate{
			ProductCode:   item.ProductCode,
			TermMonths:    item.TermMonths,
			Method:        item.Method,
			Denominator:   item.Denominator,
			Rate:          item.Rate,
			EffectiveFrom: item.EffectiveFrom,
			IsActive:      item.IsActive,
		})
	}
	return redacted
}
