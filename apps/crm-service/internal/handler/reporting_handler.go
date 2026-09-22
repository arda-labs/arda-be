package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/repository"
)

// InternalReportingCustomers serves GET /internal/reporting/customers for
// statistical-service's reporting ETL (signed caller; tenant re-checked here).
// PII is never projected — see repository.ReportingCustomer.
func (h *CustomerHandler) InternalReportingCustomers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeError(w, r, http.StatusBadRequest, "verified tenant scope is required")
		return
	}
	orgID := strings.TrimSpace(r.URL.Query().Get("org_code"))
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	items, err := h.customerRepo.ListCustomersForReporting(r.Context(), tenantID, orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"as_of": asOf, "items": toReportingCustomers(items)})
}

// reportingCustomer mirrors repository.ReportingCustomer for the wire (kept
// explicit so the ETL contract is stable even if the repository shape moves).
type reportingCustomer struct {
	CustomerCode string `json:"customer_code"`
	OrgCode      string `json:"org_code"`
	CustomerType string `json:"customer_type"`
	Status       string `json:"status"`
	Segment      string `json:"segment"`
	CustomerRank string `json:"customer_rank"`
	RiskLevel    string `json:"risk_level"`
}

func toReportingCustomers(items []repository.ReportingCustomer) []reportingCustomer {
	out := make([]reportingCustomer, 0, len(items))
	for _, item := range items {
		out = append(out, reportingCustomer{
			CustomerCode: item.CustomerCode,
			OrgCode:      item.OrgCode,
			CustomerType: item.CustomerType,
			Status:       item.Status,
			Segment:      item.Segment,
			CustomerRank: item.CustomerRank,
			RiskLevel:    item.RiskLevel,
		})
	}
	return out
}
