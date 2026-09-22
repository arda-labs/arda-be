package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// InternalReportingHandler serves the /internal/reporting/* surface consumed
// by statistical-service's reporting ETL. It is a service-to-service ETL
// source (signed caller assertion, tenant re-checked here), not a chat tool:
// it keeps org_code (the RLS/dimension key) which the AI surface drops.
type InternalReportingHandler struct {
	savings reportingSavingsSource
	ibm     reportingIBMSource
	borrows reportingIBMBorrowSource
}

// reportingSavingsSource is the slice of SettlementService the reporting
// surface needs.
type reportingSavingsSource interface {
	ListSavingsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.Savings, error)
}

// reportingIBMSource is the interbank-deposit slice of SettlementService.
type reportingIBMSource interface {
	ListIBMDepositsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.IBMReportingRow, error)
}

// reportingIBMBorrowSource is the interbank-borrow slice of SettlementService.
type reportingIBMBorrowSource interface {
	ListBorrowsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.IBMBorrowReportingRow, error)
}

func NewInternalReportingHandler(savings reportingSavingsSource, ibm reportingIBMSource, borrows reportingIBMBorrowSource) *InternalReportingHandler {
	return &InternalReportingHandler{savings: savings, ibm: ibm, borrows: borrows}
}

type reportingSavingsResponse struct {
	AsOf  string             `json:"as_of"`
	Items []reportingSavings `json:"items"`
}

// reportingSavings is the savings row exposed to the reporting ETL: the
// business fields plus org_code, but still without internal row id,
// workflow/journal linkage or audit columns.
type reportingSavings struct {
	SavingsCode    string `json:"savings_code"`
	CustomerCode   string `json:"customer_code"`
	ProductCode    string `json:"product_code"`
	OrgCode        string `json:"org_code"`
	OpenDate       string `json:"open_date"`
	MaturityDate   string `json:"maturity_date"`
	PrincipalMinor int64  `json:"principal_minor"`
	AccruedMinor   int64  `json:"accrued_minor"`
	CurrencyCode   string `json:"currency_code"`
	Status         string `json:"status"`
}

func toReportingSavings(items []repository.Savings) []reportingSavings {
	out := make([]reportingSavings, 0, len(items))
	for _, item := range items {
		out = append(out, reportingSavings{
			SavingsCode:    item.SavingsCode,
			CustomerCode:   item.CustomerCode,
			ProductCode:    item.ProductCode,
			OrgCode:        item.OrgCode,
			OpenDate:       item.OpenDate,
			MaturityDate:   item.MaturityDate,
			PrincipalMinor: item.PrincipalMinor,
			AccruedMinor:   item.AccruedMinor,
			CurrencyCode:   item.CurrencyCode,
			Status:         item.Status,
		})
	}
	return out
}

// InternalReportingSavings serves GET /internal/reporting/deposit-savings for
// statistical-service. Tenant comes from X-Tenant-Id; org_code is an optional
// narrowing filter; as_of is echoed back.
func (h *InternalReportingHandler) InternalReportingSavings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := aiTenantID(w, r)
	if !ok {
		return
	}
	orgCode := strings.TrimSpace(r.URL.Query().Get("org_code"))
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	items, err := h.savings.ListSavingsForReporting(r.Context(), tenantID, orgCode)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, reportingSavingsResponse{AsOf: asOf, Items: toReportingSavings(items)})
}

type reportingIBMResponse struct {
	AsOf  string         `json:"as_of"`
	Items []reportingIBM `json:"items"`
}

// reportingIBM is one interbank deposit exposed to the reporting ETL: the
// business fields plus org_code, without internal row id or audit columns.
type reportingIBM struct {
	DepositCode      string  `json:"deposit_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	ProductCode      string  `json:"product_code"`
	TermMonths       int     `json:"term_months"`
	DepositDate      string  `json:"deposit_date"`
	MaturityDate     string  `json:"maturity_date"`
	PrincipalMinor   int64   `json:"principal_minor"`
	AccruedMinor     int64   `json:"accrued_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	OrgCode          string  `json:"org_code"`
	Status           string  `json:"status"`
}

func toReportingIBM(items []repository.IBMReportingRow) []reportingIBM {
	out := make([]reportingIBM, 0, len(items))
	for _, item := range items {
		out = append(out, reportingIBM{
			DepositCode:      item.DepositCode,
			CounterpartyCode: item.CounterpartyCode,
			ProductCode:      item.ProductCode,
			TermMonths:       item.TermMonths,
			DepositDate:      item.DepositDate,
			MaturityDate:     item.MaturityDate,
			PrincipalMinor:   item.PrincipalMinor,
			AccruedMinor:     item.AccruedMinor,
			InterestRate:     item.InterestRate,
			CurrencyCode:     item.CurrencyCode,
			OrgCode:          item.OrgCode,
			Status:           item.Status,
		})
	}
	return out
}

// InternalReportingIBMDeposits serves GET /internal/reporting/ibm-deposits for
// statistical-service (PCF "Tiền gửi TCTD" indicators).
func (h *InternalReportingHandler) InternalReportingIBMDeposits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := aiTenantID(w, r)
	if !ok {
		return
	}
	orgCode := strings.TrimSpace(r.URL.Query().Get("org_code"))
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	items, err := h.ibm.ListIBMDepositsForReporting(r.Context(), tenantID, orgCode)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, reportingIBMResponse{AsOf: asOf, Items: toReportingIBM(items)})
}

type reportingIBMBorrowResponse struct {
	AsOf  string               `json:"as_of"`
	Items []reportingIBMBorrow `json:"items"`
}

// reportingIBMBorrow is one interbank borrowing exposed to the reporting ETL.
type reportingIBMBorrow struct {
	BorrowCode       string  `json:"borrow_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	LenderType       string  `json:"lender_type"`
	FundingPurpose   string  `json:"funding_purpose"`
	TermMonths       int     `json:"term_months"`
	DrawdownDate     string  `json:"drawdown_date"`
	MaturityDate     string  `json:"maturity_date"`
	PrincipalMinor   int64   `json:"principal_minor"`
	OutstandingMinor int64   `json:"outstanding_minor"`
	AccruedMinor     int64   `json:"accrued_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	OrgCode          string  `json:"org_code"`
	Status           string  `json:"status"`
}

func toReportingIBMBorrow(items []repository.IBMBorrowReportingRow) []reportingIBMBorrow {
	out := make([]reportingIBMBorrow, 0, len(items))
	for _, item := range items {
		out = append(out, reportingIBMBorrow{
			BorrowCode:       item.BorrowCode,
			CounterpartyCode: item.CounterpartyCode,
			LenderType:       item.LenderType,
			FundingPurpose:   item.FundingPurpose,
			TermMonths:       item.TermMonths,
			DrawdownDate:     item.DrawdownDate,
			MaturityDate:     item.MaturityDate,
			PrincipalMinor:   item.PrincipalMinor,
			OutstandingMinor: item.OutstandingMinor,
			AccruedMinor:     item.AccruedMinor,
			InterestRate:     item.InterestRate,
			CurrencyCode:     item.CurrencyCode,
			OrgCode:          item.OrgCode,
			Status:           item.Status,
		})
	}
	return out
}

// InternalReportingIBMBorrows serves GET /internal/reporting/ibm-borrows for
// statistical-service (PCF "Tiền vay TCTD" indicators).
func (h *InternalReportingHandler) InternalReportingIBMBorrows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		aiMethodNotAllowed(w, r)
		return
	}
	tenantID, ok := aiTenantID(w, r)
	if !ok {
		return
	}
	orgCode := strings.TrimSpace(r.URL.Query().Get("org_code"))
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	items, err := h.borrows.ListBorrowsForReporting(r.Context(), tenantID, orgCode)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, reportingIBMBorrowResponse{AsOf: asOf, Items: toReportingIBMBorrow(items)})
}
