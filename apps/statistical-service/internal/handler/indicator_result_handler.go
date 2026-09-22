package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/indicator"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// IndicatorResultHandler exposes the stored indicator values (step 3 of the
// reporting data layer): the compute engine or an operator writes one value
// per (indicator, period, dimension) and the reporting screens read them back.
type IndicatorResultHandler struct {
	svc indicatorResultService
}

type indicatorResultService interface {
	UpsertIndicatorResult(ctx context.Context, tenantID, actor string, in *repository.IndicatorResult) (*repository.IndicatorResult, error)
	ListIndicatorResults(ctx context.Context, params repository.ListIndicatorResultsParams) ([]repository.IndicatorResult, error)
	ComputeIndicator(ctx context.Context, tenantID, actor, code string, params map[string]string) (*repository.IndicatorResult, error)
	ComputeAllIndicators(ctx context.Context, tenantID, actor string, params map[string]string) (map[string]any, error)
	ReconcileAccountingIndicators(ctx context.Context, tenantID, periodCode string) (indicator.ReconciliationReport, error)
}

func NewIndicatorResultHandler(svc indicatorResultService) *IndicatorResultHandler {
	return &IndicatorResultHandler{svc: svc}
}

// ComputeIndicators handles POST /api/statistical/indicators/compute?code=&period_code=
// — evaluates one indicator (with `code`) or all active indicators and stores
// the results. `dim_<name>=value` slices a dimension (org, product, …).
func (h *IndicatorResultHandler) ComputeIndicators(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	params := map[string]string{"period_code": r.URL.Query().Get("period_code")}
	for _, name := range indicator.DimensionNames {
		if v := r.URL.Query().Get("dim_" + name); v != "" {
			params["dim_"+name] = v
		}
	}
	actor := r.Header.Get("X-User-Id")
	code := r.URL.Query().Get("code")
	if code != "" {
		saved, err := h.svc.ComputeIndicator(r.Context(), tenantID, actor, code, params)
		if err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, saved)
		return
	}
	summary, err := h.svc.ComputeAllIndicators(r.Context(), tenantID, actor, params)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, summary)
}

// ReconcileIndicators handles GET /api/statistical/indicators/reconcile?period_code=
// — re-checks every account-type indicator against the trial-balance fact and
// returns the mismatches. A clean report means the accounting seeds agree with
// an independent recomputation; a mismatch means a wrong account mapping.
func (h *IndicatorResultHandler) ReconcileIndicators(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	report, err := h.svc.ReconcileAccountingIndicators(r.Context(), tenantID, r.URL.Query().Get("period_code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, report)
}

// RunReconcileAccountingJob handles POST /internal/jobs/reconcile-accounting
// (called by the platform EOD engine). Tenant comes from X-Tenant-Id; the
// period from ?period_code= (falling back to to_date's month).
func (h *IndicatorResultHandler) RunReconcileAccountingJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period_code"))
	if period == "" {
		if toDate := strings.TrimSpace(r.URL.Query().Get("to_date")); len(toDate) >= 7 {
			period = toDate[:7]
		}
	}
	report, err := h.svc.ReconcileAccountingIndicators(r.Context(), tenantID, period)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	// The EOD engine fails a step on a non-2xx status, so a mismatch or an
	// unbalanced trial balance must not be reported as success: the whole point
	// of the step is to stop a wrong number before it reaches a report.
	if len(report.Mismatches) > 0 || !report.TrialBalanced {
		ardahttp.WriteSuccess(w, r, http.StatusUnprocessableEntity, report)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, report)
}

// ListIndicatorResults handles GET /api/statistical/indicator-results.
func (h *IndicatorResultHandler) ListIndicatorResults(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	revision := 0
	if raw := r.URL.Query().Get("revision"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			revision = n
		}
	}
	items, err := h.svc.ListIndicatorResults(r.Context(), repository.ListIndicatorResultsParams{
		TenantID:      tenantID,
		PeriodCode:    r.URL.Query().Get("period_code"),
		IndicatorCode: r.URL.Query().Get("indicator_code"),
		Revision:      revision,
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertIndicatorResult handles POST/PUT /api/statistical/indicator-results.
func (h *IndicatorResultHandler) UpsertIndicatorResult(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	var in repository.IndicatorResult
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	saved, err := h.svc.UpsertIndicatorResult(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, saved)
}
