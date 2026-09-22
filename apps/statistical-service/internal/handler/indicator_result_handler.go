package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

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
