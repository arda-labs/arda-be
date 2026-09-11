package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// ReportingHandler exposes the P3a reporting foundation: the COB
// trial-balance rebuild job (called by the platform EOD engine) and the
// statement/report read API over the precomputed daily balances
// (fac-statistical-reporting-survey.md §5.2).
type ReportingHandler struct {
	daily *service.TrialBalanceDailyService
	stmts *service.StatementService
}

func NewReportingHandler(daily *service.TrialBalanceDailyService, stmts *service.StatementService) *ReportingHandler {
	return &ReportingHandler{daily: daily, stmts: stmts}
}

// RunTrialBalanceDailyJob handles POST /internal/jobs/trial-balance-daily?to_date=YYYY-MM-DD.
// Tenant comes from X-Tenant-Id forwarded by the EOD engine; actor is the
// job identity. Rebuild is idempotent per (tenant, date) — re-running a
// failed COB date or backfilling is always safe.
func (h *ReportingHandler) RunTrialBalanceDailyJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	toDate := r.URL.Query().Get("to_date")
	actor := strings.TrimSpace(r.Header.Get("X-User-Id"))
	result, err := h.daily.RebuildDaily(r.Context(), tenantID, toDate, actor)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, result)
}

// TrialBalanceDaily handles GET /api/finance/trial-balance/daily?as_of= —
// the precomputed per-account balances for one date (read API for the FE
// trial-balance grid and statement screens).
func (h *ReportingHandler) TrialBalanceDaily(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	entries, err := h.daily.ListDaily(r.Context(), tenantID, r.URL.Query().Get("as_of"))
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"as_of":     r.URL.Query().Get("as_of"),
		"entries":   entries,
	})
}

// ListStatements handles GET /api/finance/statements — the statement codes
// available to the tenant (from fin_statement_formula).
func (h *ReportingHandler) ListStatements(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	codes, err := h.stmts.ListStatements(r.Context(), tenantID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"statements": codes})
}

// GetFinancialSummary handles GET /api/finance/reports/financial-summary —
// consolidated CDKT + B02 totals (Tổng hợp báo cáo tài chính).
func (h *ReportingHandler) GetFinancialSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	summary, err := h.stmts.FinancialSummary(r.Context(), tenantID,
		r.URL.Query().Get("as_of"), r.URL.Query().Get("coa_version"), r.URL.Query().Get("from"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, summary)
}

// RunStatement handles GET /api/finance/statements/{code}/run?as_of=&coa_version=
// — renders the statement rows from fin_trial_balance_daily (read-only,
// idempotent; amounts are minor units, debit-positive unless sign flips).
func (h *ReportingHandler) RunStatement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	code := r.PathValue("code")
	result, err := h.stmts.RunStatement(r.Context(), tenantID, code,
		r.URL.Query().Get("as_of"), r.URL.Query().Get("coa_version"), r.URL.Query().Get("from"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, result)
}

// ExportStatement handles GET /api/finance/statements/{code}/export?as_of=&coa_version=
// — renders the statement and streams an XLSX attachment (sync blob; sizes
// are tens of rows so no async job handoff is warranted yet).
func (h *ReportingHandler) ExportStatement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	code := r.PathValue("code")
	result, err := h.stmts.RunStatement(r.Context(), tenantID, code,
		r.URL.Query().Get("as_of"), r.URL.Query().Get("coa_version"), r.URL.Query().Get("from"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	xlsx, err := service.ToExcel(result)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.xlsx",
		strings.ToLower(code), result.AsOf))
	w.Header().Set("Content-Length", strconv.Itoa(len(xlsx)))
	_, _ = w.Write(xlsx)
}
