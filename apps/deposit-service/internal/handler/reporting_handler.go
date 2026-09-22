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
}

// reportingSavingsSource is the slice of SettlementService the reporting
// surface needs.
type reportingSavingsSource interface {
	ListSavingsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.Savings, error)
}

func NewInternalReportingHandler(savings reportingSavingsSource) *InternalReportingHandler {
	return &InternalReportingHandler{savings: savings}
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
