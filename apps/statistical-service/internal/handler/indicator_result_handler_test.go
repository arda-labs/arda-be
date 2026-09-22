package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/indicator"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
)

type stubIndicatorResultService struct {
	saved     *repository.IndicatorResult
	seenTen   string
	seenActor string
	report    *indicator.ReconciliationReport
}

func (s *stubIndicatorResultService) UpsertIndicatorResult(_ context.Context, tenantID, actor string, in *repository.IndicatorResult) (*repository.IndicatorResult, error) {
	s.seenTen = tenantID
	s.seenActor = actor
	s.saved = in
	return in, nil
}

func (s *stubIndicatorResultService) ListIndicatorResults(_ context.Context, _ repository.ListIndicatorResultsParams) ([]repository.IndicatorResult, error) {
	return []repository.IndicatorResult{}, nil
}

func (s *stubIndicatorResultService) ComputeIndicator(_ context.Context, tenantID, actor, code string, params map[string]string) (*repository.IndicatorResult, error) {
	s.seenTen = tenantID
	s.seenActor = actor
	v := 0.0
	return &repository.IndicatorResult{IndicatorCode: code, PeriodCode: params["period_code"], Value: &v}, nil
}

func (s *stubIndicatorResultService) ComputeAllIndicators(_ context.Context, _, _ string, _ map[string]string) (map[string]any, error) {
	return map[string]any{"computed_count": 0, "failed_count": 0}, nil
}

func (s *stubIndicatorResultService) ReconcileAccountingIndicators(_ context.Context, tenantID, periodCode string) (indicator.ReconciliationReport, error) {
	s.seenTen = tenantID
	if s.report != nil {
		return *s.report, nil
	}
	return indicator.ReconciliationReport{PeriodCode: periodCode, Checked: 0, TrialBalanced: true}, nil
}

// TestReconcileJobFailsTheStepOnMismatch: the EOD engine treats a non-2xx status
// as a failed step, so a mismatch or an unbalanced trial balance must not be
// reported as success — that is the whole point of running the check at COB.
func TestReconcileJobFailsTheStepOnMismatch(t *testing.T) {
	h := NewIndicatorResultHandler(&stubIndicatorResultService{
		report: &indicator.ReconciliationReport{
			Checked:    2,
			Mismatches: []indicator.ReconcileResult{{Code: "40001.01"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/internal/jobs/reconcile-accounting?to_date=2026-09-22", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	res := httptest.NewRecorder()
	h.RunReconcileAccountingJob(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatch status = %d, want %d", res.Code, http.StatusUnprocessableEntity)
	}
}

// TestReconcileJobSucceedsWhenCleanAndDerivesPeriod: a clean run is 200 and the
// period is taken from to_date when period_code is absent.
func TestReconcileJobSucceedsWhenCleanAndDerivesPeriod(t *testing.T) {
	stub := &stubIndicatorResultService{}
	h := NewIndicatorResultHandler(stub)
	req := httptest.NewRequest(http.MethodPost, "/internal/jobs/reconcile-accounting?to_date=2026-09-22", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	res := httptest.NewRecorder()
	h.RunReconcileAccountingJob(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("clean status = %d, want %d (body %s)", res.Code, http.StatusOK, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"period_code":"2026-09"`) {
		t.Fatalf("period not derived from to_date: %s", res.Body.String())
	}
}

func TestReconcileIndicatorsRequiresTenant(t *testing.T) {
	h := NewIndicatorResultHandler(&stubIndicatorResultService{})
	req := httptest.NewRequest(http.MethodGet, "/api/statistical/indicators/reconcile?period_code=2026-09", nil)
	res := httptest.NewRecorder()
	h.ReconcileIndicators(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestIndicatorResult_TenantRequired(t *testing.T) {
	h := NewIndicatorResultHandler(&stubIndicatorResultService{})

	req := httptest.NewRequest(http.MethodGet, "/api/statistical/indicator-results?period_code=2026-09", nil)
	res := httptest.NewRecorder()
	h.ListIndicatorResults(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestIndicatorResult_UpsertPassesTenantAndActor(t *testing.T) {
	stub := &stubIndicatorResultService{}
	h := NewIndicatorResultHandler(stub)

	body := `{"indicator_code":"20000.01","period_code":"2026-09","dimension_key":"org=01","value":123.45}`
	req := httptest.NewRequest(http.MethodPost, "/api/statistical/indicator-results", strings.NewReader(body))
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "analyst-1")
	res := httptest.NewRecorder()
	h.UpsertIndicatorResult(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", res.Code, http.StatusOK, res.Body.String())
	}
	if stub.seenTen != "tenant-1" || stub.seenActor != "analyst-1" {
		t.Fatalf("service saw (%q, %q), want (tenant-1, analyst-1)", stub.seenTen, stub.seenActor)
	}
	if stub.saved == nil || stub.saved.IndicatorCode != "20000.01" || stub.saved.Value == nil || *stub.saved.Value != 123.45 {
		t.Fatalf("decoded result wrong: %+v", stub.saved)
	}
}
