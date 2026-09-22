package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
)

type stubIndicatorResultService struct {
	saved     *repository.IndicatorResult
	seenTen   string
	seenActor string
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
