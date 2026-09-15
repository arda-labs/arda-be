package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// aiMaxQueryLen caps the free-text search the assistant can submit; it matches
// the q parameter schema in contracts/ai-internal/capital-v1.json.
const aiMaxQueryLen = 128

// aiListSpec is the AI tool list contract: per_page 1..20 with default 10.
// all=1 and views are rejected so the assistant can never pull unbounded
// pages; sort is not exposed on the reference catalogs.
var aiListSpec = ardahttp.ListSpec{
	DefaultPerPage: 10,
	MaxPerPage:     20,
}

// aiContractListSpec additionally allows the repository-whitelisted contract
// sort columns (contract_code, contract_date); anything else is a 400.
var aiContractListSpec = ardahttp.ListSpec{
	DefaultPerPage: 10,
	MaxPerPage:     20,
	SortFields:     []string{"contract_code", "contract_date"},
}

// aiContractStatuses mirrors the cfc_contracts lifecycle states. The contract
// declares the same enum, so invalid statuses are rejected twice: once by the
// sandbox argument validation and again here for direct HTTP callers.
var aiContractStatuses = []string{"PENDING_APPROVAL", "ACTIVE", "REJECTED", "CLOSED"}

// aiCapitalSource is the read slice of CapitalService the AI surface needs.
// The interface form keeps the handler testable without a database while the
// concrete *service.CapitalService satisfies it as-is.
type aiCapitalSource interface {
	ListFundTypes(ctx context.Context, tenantID string, includeInactive bool) ([]repository.FundType, error)
	ListProducts(ctx context.Context, tenantID string, includeInactive bool) ([]repository.CapitalProduct, error)
	ListContracts(ctx context.Context, params repository.ListContractsParams) ([]repository.CapitalContract, int, error)
}

// InternalAIHandler serves the /internal/ai/* surface consumed by ai-service.
// The signed caller assertion is verified by the router's internalAIService
// middleware; the delegated subject (X-Tenant-Id / X-User-Org-Ids) is
// re-validated here and forwarded to the repository, so a tenant can never see
// another tenant's capital data. Every response is a redacted allowlist —
// tenant_id, workflow/journal linkage, actor ids and timestamps never leave
// this handler.
type InternalAIHandler struct {
	source aiCapitalSource
}

func NewInternalAIHandler(source aiCapitalSource) *InternalAIHandler {
	return &InternalAIHandler{source: source}
}

// InternalAIListFundTypes serves GET /internal/ai/fund-types for ai-service.
// Fund types are a small reference catalog: q narrows by code/name and the
// redacted slice is paged in memory. only active rows are exposed.
func (h *InternalAIHandler) InternalAIListFundTypes(w http.ResponseWriter, r *http.Request) {
	tenantID, listReq, ok := aiListRequest(w, r, aiListSpec)
	if !ok {
		return
	}
	items, err := h.source.ListFundTypes(r.Context(), tenantID, false)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	filtered := aiFilterByQuery(items, aiQuery(r),
		func(t repository.FundType) string { return t.Code },
		func(t repository.FundType) string { return t.Name })
	paged, total, page, perPage := ardahttp.PageSlice(filtered, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, toAIFundTypes(paged)))
}

// InternalAIListProducts serves GET /internal/ai/products for ai-service.
// Term, rate and currency are exposed; tenant_id, actor ids and timestamps are
// dropped by toAICapitalProducts.
func (h *InternalAIHandler) InternalAIListProducts(w http.ResponseWriter, r *http.Request) {
	tenantID, listReq, ok := aiListRequest(w, r, aiListSpec)
	if !ok {
		return
	}
	items, err := h.source.ListProducts(r.Context(), tenantID, false)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	filtered := aiFilterByQuery(items, aiQuery(r),
		func(p repository.CapitalProduct) string { return p.Code },
		func(p repository.CapitalProduct) string { return p.Name })
	paged, total, page, perPage := ardahttp.PageSlice(filtered, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, toAICapitalProducts(paged)))
}

// InternalAIListContracts serves GET /internal/ai/contracts for ai-service.
// It is a paged, redacted read of the fund-contract register; the optional
// status filter is allowlisted and the org scope is forwarded from the
// delegated subject headers (X-Org-Id / X-User-Org-Ids), never from tool
// arguments. The repository applies the tenant filter again.
func (h *InternalAIHandler) InternalAIListContracts(w http.ResponseWriter, r *http.Request) {
	tenantID, listReq, ok := aiListRequest(w, r, aiContractListSpec)
	if !ok {
		return
	}
	status, ok := aiContractStatus(w, r)
	if !ok {
		return
	}
	page := listReq.Page
	if page < 1 {
		page = 1
	}
	items, total, err := h.source.ListContracts(r.Context(), repository.ListContractsParams{
		TenantID: tenantID,
		OrgCodes: orgScopeFromCap(r).listFilter(),
		Status:   status,
		Q:        aiQuery(r),
		Sort:     listReq.Sort,
		Order:    listReq.Order,
		Page:     (page - 1) * listReq.PerPage,
		Size:     listReq.PerPage,
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, listReq.PerPage, total, toAIContracts(items)))
}

// aiListRequest enforces the shared AI list preconditions: GET only, the
// verified tenant from the delegated X-Tenant-Id header and a bounded
// page/per_page/q query. The tenant is never read from the query string.
func aiListRequest(w http.ResponseWriter, r *http.Request, spec ardahttp.ListSpec) (string, ardahttp.ListRequest, bool) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return "", ardahttp.ListRequest{}, false
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "verified tenant scope is required"))
		return "", ardahttp.ListRequest{}, false
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), spec)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return "", ardahttp.ListRequest{}, false
	}
	return tenantID, listReq, true
}

// aiContractStatus normalizes the optional status filter and rejects values
// outside the contract lifecycle.
func aiContractStatus(w http.ResponseWriter, r *http.Request) (string, bool) {
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		return "", true
	}
	for _, known := range aiContractStatuses {
		if status == known {
			return status, true
		}
	}
	ardahttp.WriteProblem(w, r, http.StatusBadRequest,
		ardaerrors.New(ardaerrors.CodeInvalidInput, "status must be one of: "+strings.Join(aiContractStatuses, ", ")))
	return "", false
}

// aiQuery trims the free-text search and clamps it to aiMaxQueryLen
// (rune-safe: the query can carry Vietnamese text).
func aiQuery(r *http.Request) string {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	runes := []rune(q)
	if len(runes) > aiMaxQueryLen {
		return string(runes[:aiMaxQueryLen])
	}
	return q
}

// aiFilterByQuery keeps the items whose allowlisted keys contain the
// (case-insensitive) free-text query; an empty query keeps everything.
func aiFilterByQuery[T any](items []T, query string, keys ...func(T) string) []T {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		for _, key := range keys {
			if strings.Contains(strings.ToLower(key(item)), query) {
				out = append(out, item)
				break
			}
		}
	}
	return out
}

// aiFundType is the redacted fund-type shape exposed to the AI SDK. tenant_id,
// created_by and timestamps are dropped here and again by the response
// allowlist in contracts/ai-internal/capital-v1.json.
type aiFundType struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

func toAIFundTypes(items []repository.FundType) []aiFundType {
	redacted := make([]aiFundType, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiFundType{
			ID:       item.ID,
			Code:     item.Code,
			Name:     item.Name,
			IsActive: item.IsActive,
		})
	}
	return redacted
}

// aiCapitalProduct is the redacted fund-product shape. Pricing fields (term,
// rate, currency) are safe to expose; actor ids and timestamps are not.
type aiCapitalProduct struct {
	ID           string  `json:"id"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	FundTypeCode string  `json:"fund_type_code"`
	TermMonths   int     `json:"term_months"`
	InterestRate float64 `json:"interest_rate"`
	CurrencyCode string  `json:"currency_code"`
	IsActive     bool    `json:"is_active"`
}

func toAICapitalProducts(items []repository.CapitalProduct) []aiCapitalProduct {
	redacted := make([]aiCapitalProduct, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiCapitalProduct{
			ID:           item.ID,
			Code:         item.Code,
			Name:         item.Name,
			FundTypeCode: item.FundTypeCode,
			TermMonths:   item.TermMonths,
			InterestRate: item.InterestRate,
			CurrencyCode: item.CurrencyCode,
			IsActive:     item.IsActive,
		})
	}
	return redacted
}

// aiCapitalContract is the redacted fund-contract shape. Contract economics
// and status stay; tenant_id, workflow_case_id, journal_entry_id, created_by
// and timestamps are dropped. counterparty_code is a business code (never a
// person name or account number), matching the CRM AI surface's customerCode.
type aiCapitalContract struct {
	ID               string  `json:"id"`
	ContractCode     string  `json:"contract_code"`
	FundTypeCode     string  `json:"fund_type_code"`
	ProductCode      string  `json:"product_code,omitempty"`
	CounterpartyCode string  `json:"counterparty_code"`
	ContractDate     string  `json:"contract_date"`
	MaturityDate     string  `json:"maturity_date,omitempty"`
	AmountMinor      int64   `json:"amount_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	Status           string  `json:"status"`
	OrgCode          string  `json:"org_code,omitempty"`
}

func toAIContracts(items []repository.CapitalContract) []aiCapitalContract {
	redacted := make([]aiCapitalContract, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiCapitalContract{
			ID:               item.ID,
			ContractCode:     item.ContractCode,
			FundTypeCode:     item.FundTypeCode,
			ProductCode:      item.ProductCode,
			CounterpartyCode: item.CounterpartyCode,
			ContractDate:     item.ContractDate,
			MaturityDate:     item.MaturityDate,
			AmountMinor:      item.AmountMinor,
			InterestRate:     item.InterestRate,
			CurrencyCode:     item.CurrencyCode,
			Status:           item.Status,
			OrgCode:          item.OrgCode,
		})
	}
	return redacted
}
