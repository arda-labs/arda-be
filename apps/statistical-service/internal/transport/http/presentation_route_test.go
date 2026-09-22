package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/handler"
	"github.com/arda-labs/arda/apps/statistical-service/internal/presentation"
)

type routePresentation struct{}

func (routePresentation) ReportChart(context.Context, string, string, map[string]string) (presentation.Chart, error) {
	return presentation.Chart{Type: presentation.ChartBar, Categories: []string{"a"}}, nil
}

func (routePresentation) ReportDocument(context.Context, string, string, map[string]string) (*presentation.ReportDocument, error) {
	return &presentation.ReportDocument{Title: "T", Period: "2026-09", Columns: []string{"a"}, Rows: [][]any{{1}}}, nil
}

func (routePresentation) IndicatorDocument(context.Context, string, string) (*presentation.ReportDocument, error) {
	return &presentation.ReportDocument{Title: "I", Period: "2026-09"}, nil
}

func (routePresentation) RenderDocument(context.Context, *presentation.ReportDocument, string) ([]byte, string, error) {
	return []byte("%PDF-1.4 test"), "application/pdf", nil
}

// TestPresentationRoutes exercises the real ServeMux so the {code} wildcard and
// the download disposition are covered (a bare NewRequest never sets PathValue).
func TestPresentationRoutes(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, handler.NewPresentationHandler(routePresentation{}))

	tests := []struct {
		name   string
		path   string
		wantCt string
		wantIn string
	}{
		{
			name:   "chart",
			path:   "/api/statistical/reports/LOAN_PORTFOLIO_SUMMARY/chart?period_code=2026-09",
			wantCt: "application/json",
			wantIn: `"type":"bar"`,
		},
		{
			name:   "document",
			path:   "/api/statistical/reports/LOAN_PORTFOLIO_SUMMARY/document?period_code=2026-09&format=pdf",
			wantCt: "application/pdf",
			wantIn: "LOAN_PORTFOLIO_SUMMARY-2026-09.pdf",
		},
		{
			name:   "indicator document",
			path:   "/api/statistical/indicators/document?period_code=2026-09&format=pdf",
			wantCt: "application/pdf",
			wantIn: "indicators-2026-09.pdf",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("X-Tenant-Id", "tenant-1")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", res.Code, res.Body.String())
			}
			if ct := res.Header().Get("Content-Type"); ct != tt.wantCt {
				t.Fatalf("content-type = %q, want %q", ct, tt.wantCt)
			}
			haystack := res.Body.String() + res.Header().Get("Content-Disposition")
			if !strings.Contains(haystack, tt.wantIn) {
				t.Fatalf("response missing %q: body=%s disposition=%s", tt.wantIn, res.Body.String(), res.Header().Get("Content-Disposition"))
			}
		})
	}
}
