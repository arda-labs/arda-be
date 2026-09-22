package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/reporting"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// ReportingJobHandler exposes the reporting ETL job endpoint called by the
// platform EOD engine (same internal-job contract as the other COB steps:
// POST /internal/jobs/<step>?to_date=YYYY-MM-DD, tenant in X-Tenant-Id).
type ReportingJobHandler struct {
	svc *reporting.Service
}

func NewReportingJobHandler(svc *reporting.Service) *ReportingJobHandler {
	return &ReportingJobHandler{svc: svc}
}

// RunReportExtractDaily handles POST /internal/jobs/report-extract-daily.
// Idempotent per (tenant, business_date); re-running a failed COB date or
// backfilling is always safe.
func (h *ReportingJobHandler) RunReportExtractDaily(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	businessDate := strings.TrimSpace(r.URL.Query().Get("to_date"))
	if businessDate == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "to_date is required"))
		return
	}
	result, err := h.svc.ExtractDaily(r.Context(), tenantID, businessDate)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadGateway, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, result)
}
