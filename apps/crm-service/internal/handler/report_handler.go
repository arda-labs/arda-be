package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// ReportHandler exposes the CRM report reads (W4c).
type ReportHandler struct {
	svc *service.ReportService
}

func NewReportHandler(svc *service.ReportService) *ReportHandler {
	return &ReportHandler{svc: svc}
}

// GetCustomerReport handles GET /api/crm/reports/customers.
func (h *ReportHandler) GetCustomerReport(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	q := r.URL.Query()
	items, err := h.svc.CustomerReport(r.Context(), tenantID, q.Get("q"), q.Get("customer_type"), q.Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}
