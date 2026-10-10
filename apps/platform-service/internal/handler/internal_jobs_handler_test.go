package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/service"
)

type fakeEOD struct {
	date string
	res  *service.RunResult
	err  error
}

func (f *fakeEOD) RunSystem(_ context.Context, d string) (*service.RunResult, error) {
	f.date = d
	return f.res, f.err
}

type fakeCleaner struct{}

func (fakeCleaner) Cleanup(context.Context, time.Time) (int64, int64, error) { return 3, 1, nil }

type failingCleaner struct{}

func (failingCleaner) Cleanup(context.Context, time.Time) (int64, int64, error) {
	return 0, 0, errors.New("db down")
}

func callInternal(t *testing.T, h *InternalJobsHandler, path, body string) map[string]any {
	t.Helper()
	mux := http.NewServeMux()
	RegisterInternalJobRoutes(mux, h, func(n http.Handler) http.Handler { return n })
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("job endpoints always answer 200 with a status, got %d", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("job response is not JSON: %v: %s", err, rec.Body.String())
	}
	return out
}

func TestInternalEODJob(t *testing.T) {
	ok := &fakeEOD{res: &service.RunResult{BusinessDate: "2026-10-10", Steps: []service.StepResult{{JobCode: "LNM_ACCRUAL", Status: "DONE"}}}}
	out := callInternal(t, NewInternalJobsHandler(ok, fakeCleaner{}), "/internal/jobs/eod", `{"params":{"business_date":"2026-10-10"}}`)
	if out["status"] != "succeeded" || ok.date != "2026-10-10" {
		t.Fatalf("unexpected %v date=%s", out, ok.date)
	}
	failedStep := &fakeEOD{res: &service.RunResult{Steps: []service.StepResult{{JobCode: "FIN_TB", Status: "FAILED", Error: "boom"}}}}
	out = callInternal(t, NewInternalJobsHandler(failedStep, fakeCleaner{}), "/internal/jobs/eod", `{}`)
	if out["status"] != "failed" || out["error_code"] != "EOD_STEP_FAILED" || !strings.Contains(out["error_message"].(string), "FIN_TB") {
		t.Fatalf("unexpected %v", out)
	}
	errRun := &fakeEOD{err: errors.New("lock held")}
	out = callInternal(t, NewInternalJobsHandler(errRun, fakeCleaner{}), "/internal/jobs/eod", `{}`)
	if out["status"] != "failed" || out["error_code"] != "EOD_RUN_FAILED" {
		t.Fatalf("unexpected %v", out)
	}
}

func TestInternalCleanupJob(t *testing.T) {
	out := callInternal(t, NewInternalJobsHandler(&fakeEOD{}, fakeCleaner{}), "/internal/jobs/platform-runs-cleanup", `{}`)
	summary, _ := out["summary"].(map[string]any)
	if out["status"] != "succeeded" || summary["runs_deleted"] != float64(3) || summary["outbox_deleted"] != float64(1) {
		t.Fatalf("unexpected %v", out)
	}
	out = callInternal(t, NewInternalJobsHandler(&fakeEOD{}, failingCleaner{}), "/internal/jobs/platform-runs-cleanup", `{}`)
	if out["status"] != "failed" || out["error_code"] != "CLEANUP_FAILED" || out["error_message"] != "db down" {
		t.Fatalf("unexpected %v", out)
	}
}
