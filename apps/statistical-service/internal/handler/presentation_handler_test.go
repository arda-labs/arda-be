package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/presentation"
)

type stubPresentation struct {
	chartErr error
	doc      *presentation.ReportDocument
	body     []byte
	format   string
}

func (s *stubPresentation) ReportChart(_ context.Context, _, _ string, _ map[string]string) (presentation.Chart, error) {
	if s.chartErr != nil {
		return presentation.Chart{}, s.chartErr
	}
	return presentation.Chart{Type: presentation.ChartBar, Categories: []string{"a"}}, nil
}

func (s *stubPresentation) ReportDocument(_ context.Context, _, _ string, _ map[string]string) (*presentation.ReportDocument, error) {
	if s.doc != nil {
		return s.doc, nil
	}
	return &presentation.ReportDocument{Title: "T", Period: "2026-09", Columns: []string{"a"}, Rows: [][]any{{1}}}, nil
}

func (s *stubPresentation) IndicatorDocument(_ context.Context, _, period string) (*presentation.ReportDocument, error) {
	return &presentation.ReportDocument{Title: "I", Period: period}, nil
}

func (s *stubPresentation) RenderDocument(_ context.Context, _ *presentation.ReportDocument, format string) ([]byte, string, error) {
	s.format = format
	if s.body != nil {
		return s.body, "application/pdf", nil
	}
	return []byte("%PDF-1.4 test"), "application/pdf", nil
}

func TestPresentation_TenantRequired(t *testing.T) {
	h := NewPresentationHandler(&stubPresentation{})

	req := httptest.NewRequest(http.MethodGet, "/api/statistical/reports/X/chart?period_code=2026-09", nil)
	res := httptest.NewRecorder()
	h.ReportChart(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestPresentation_ChartReturnsContract(t *testing.T) {
	h := NewPresentationHandler(&stubPresentation{})
	req := httptest.NewRequest(http.MethodGet, "/api/statistical/reports/X/chart?period_code=2026-09", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	res := httptest.NewRecorder()
	h.ReportChart(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"type":"bar"`) {
		t.Fatalf("chart body wrong: %s", res.Body.String())
	}
}

func TestPresentation_DocumentStreamsFileWithDisposition(t *testing.T) {
	// Go's ServeMux populates PathValue; a bare NewRequest does not, so this
	// case lives in the transport package where the real router is reachable
	// (see transport/http/presentation_route_test.go).
	h := NewPresentationHandler(&stubPresentation{})
	req := httptest.NewRequest(http.MethodGet, "/api/statistical/indicators/document?period_code=2026-09&format=xlsx", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	res := httptest.NewRecorder()
	h.IndicatorDocument(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.Code, res.Body.String())
	}
	if cd := res.Header().Get("Content-Disposition"); !strings.Contains(cd, "indicators-2026-09.xlsx") {
		t.Fatalf("disposition = %q", cd)
	}
}

func TestSanitizeFileKeepsConservativeNames(t *testing.T) {
	if got := sanitizeFile("LOAN_PORTFOLIO_SUMMARY"); got != "LOAN_PORTFOLIO_SUMMARY" {
		t.Fatalf("name mangled: %q", got)
	}
	if got := sanitizeFile("../../etc/passwd"); strings.ContainsAny(got, "/\\.") {
		t.Fatalf("path characters survived: %q", got)
	}
	if got := sanitizeFile(""); got != "report" {
		t.Fatalf("empty name fallback = %q", got)
	}
}
