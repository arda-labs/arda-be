package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// ReportHandler exposes the loan report reads (W4c).
type ReportHandler struct {
	svc *service.LoanReportService
}

func NewReportHandler(svc *service.LoanReportService) *ReportHandler {
	return &ReportHandler{svc: svc}
}

// GetLoanLedger handles GET /api/loan/reports/loan-ledger.
func (h *ReportHandler) GetLoanLedger(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenReport(w, r)
		return
	}
	q := r.URL.Query()
	items, err := h.svc.LoanLedger(r.Context(), tenantID, q.Get("from"), q.Get("to"), q.Get("contract_code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// GetLoanStatement handles GET /api/loan/reports/loan-statement.
func (h *ReportHandler) GetLoanStatement(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenReport(w, r)
		return
	}
	items, err := h.svc.LoanStatement(r.Context(), tenantID, r.URL.Query().Get("contract_code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// GetCollateralStatement handles GET /api/loan/reports/collateral-statement.
func (h *ReportHandler) GetCollateralStatement(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenReport(w, r)
		return
	}
	items, err := h.svc.CollateralStatement(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

func writeForbiddenReport(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
}
