package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/reporting"
)

// TestRunReportExtractDaily_Guards locks the job contract before any ETL work:
// POST only, tenant required, to_date required.
func TestRunReportExtractDaily_Guards(t *testing.T) {
	h := NewReportingJobHandler(reporting.NewService(nil, "secret", "", "", "", "", nil))

	tests := []struct {
		name   string
		method string
		path   string
		tenant string
		status int
	}{
		{name: "wrong method", method: http.MethodGet, path: "/internal/jobs/report-extract-daily?to_date=2026-09-22", tenant: "t1", status: http.StatusMethodNotAllowed},
		{name: "missing tenant", method: http.MethodPost, path: "/internal/jobs/report-extract-daily?to_date=2026-09-22", status: http.StatusForbidden},
		{name: "missing to_date", method: http.MethodPost, path: "/internal/jobs/report-extract-daily", tenant: "t1", status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.tenant != "" {
				req.Header.Set("X-Tenant-Id", tt.tenant)
			}
			res := httptest.NewRecorder()
			h.RunReportExtractDaily(res, req)
			if res.Code != tt.status {
				t.Errorf("status = %d, want %d (body %s)", res.Code, tt.status, res.Body.String())
			}
		})
	}
}
