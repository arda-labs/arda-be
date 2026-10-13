package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
)

type fakeJobs struct {
	updateErr  error
	runErr     error
	lastActor  string
	lastTenant string
	lastLimit  int
	lastOffset int
}

func (f *fakeJobs) List(context.Context) ([]domain.ScheduledJob, error) {
	return []domain.ScheduledJob{{Code: "ai-eval"}}, nil
}
func (f *fakeJobs) Get(_ context.Context, code string) (*domain.ScheduledJob, error) {
	if code != "ai-eval" {
		return nil, service.ErrJobNotFound
	}
	return &domain.ScheduledJob{Code: code}, nil
}
func (f *fakeJobs) Update(_ context.Context, code string, _ domain.JobConfigUpdate, tenant, actor string) (*domain.ScheduledJob, error) {
	f.lastActor, f.lastTenant = actor, tenant
	return &domain.ScheduledJob{Code: code}, f.updateErr
}
func (f *fakeJobs) RunNow(_ context.Context, code, actor string, _ map[string]string) (*domain.ScheduledJobRun, error) {
	return &domain.ScheduledJobRun{ID: "r1", JobCode: code, Status: domain.JobStatusRunning}, f.runErr
}
func (f *fakeJobs) Preview(expr, tz string) ([]time.Time, error) {
	return domain.PreviewRuns(expr, tz, time.Now(), 5)
}
func (f *fakeJobs) ListRuns(_ context.Context, _ string, _ string, limit, offset int) ([]domain.ScheduledJobRun, int, error) {
	f.lastLimit, f.lastOffset = limit, offset
	return nil, 0, nil
}
func (f *fakeJobs) GetRun(_ context.Context, id string) (*domain.ScheduledJobRun, error) {
	return nil, service.ErrJobNotFound
}

func serveJobs(t *testing.T, f *fakeJobs, method, path, body string, admin bool) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterScheduledJobRoutes(mux, NewScheduledJobHandler(f))
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Tenant-Id", "tenant-x")
	req.Header.Set("X-User-Id", "user-1")
	if admin {
		req.Header.Set("X-Auth-Checked", "true")
		req.Header.Set("X-Global-Admin", "true")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestScheduledJobHandlerStatusMapping(t *testing.T) {
	f := &fakeJobs{}
	body := `{"cron_expr":"0 6 * * *","timezone":"Asia/Ho_Chi_Minh","enabled":true,"timeout_seconds":600,"max_attempts":2,"retry_backoff_seconds":0,"version":1}`
	cases := []struct {
		name, method, path, body string
		setup                    func()
		want                     int
		wantCode                 string
	}{
		{"list", "GET", "/api/platform/scheduled-jobs", "", nil, 200, ""},
		{"get missing", "GET", "/api/platform/scheduled-jobs/missing", "", nil, 404, "platform.jobs.not_found"},
		{"patch ok", "PATCH", "/api/platform/scheduled-jobs/ai-eval", body, nil, 200, ""},
		{"patch bad json", "PATCH", "/api/platform/scheduled-jobs/ai-eval", "{", nil, 400, "validation.invalid_json"},
		{"patch conflict", "PATCH", "/api/platform/scheduled-jobs/ai-eval", body, func() { f.updateErr = service.ErrJobVersionConflict }, 409, "platform.jobs.version_conflict"},
		{"patch bad cron", "PATCH", "/api/platform/scheduled-jobs/ai-eval", body, func() { f.updateErr = domain.ErrInvalidCron }, 400, "platform.jobs.invalid_schedule"},
		{"run running", "POST", "/api/platform/scheduled-jobs/ai-eval/run", "{}", func() { f.updateErr = nil; f.runErr = service.ErrJobAlreadyRunning }, 409, "platform.jobs.already_running"},
		{"run no target", "POST", "/api/platform/scheduled-jobs/ai-eval/run", "{}", func() { f.runErr = service.ErrJobTargetNotConfigured }, 422, "platform.jobs.target_not_configured"},
		{"run param not allowed", "POST", "/api/platform/scheduled-jobs/ai-eval/run", `{"params":{"x":"1"}}`, func() { f.runErr = service.ErrJobParamNotAllowed }, 400, "platform.jobs.param_not_allowed"},
		{"run busy", "POST", "/api/platform/scheduled-jobs/ai-eval/run", "{}", func() { f.runErr = service.ErrSchedulerBusy }, 409, "platform.jobs.busy"},
		{"run retired", "POST", "/api/platform/scheduled-jobs/ai-eval/run", "{}", func() { f.runErr = service.ErrJobRetired }, 409, "platform.jobs.retired"},
		{"run internal", "POST", "/api/platform/scheduled-jobs/ai-eval/run", "{}", func() { f.runErr = context.DeadlineExceeded }, 500, "common.error.internal"},
		{"run ok", "POST", "/api/platform/scheduled-jobs/ai-eval/run", `{"params":{}}`, func() { f.runErr = nil }, 202, ""},
		{"run ok empty body", "POST", "/api/platform/scheduled-jobs/ai-eval/run", "", nil, 202, ""},
		{"preview bad", "POST", "/api/platform/scheduled-jobs/cron-preview", `{"cron_expr":"* *","timezone":"UTC"}`, nil, 400, "platform.jobs.invalid_schedule"},
		{"preview ok", "POST", "/api/platform/scheduled-jobs/cron-preview", `{"cron_expr":"0 6 * * *","timezone":"UTC"}`, nil, 200, ""},
		{"run detail bad id", "GET", "/api/platform/scheduled-jobs/runs/not-a-uuid", "", nil, 400, "validation.invalid_input"},
		{"run detail missing", "GET", "/api/platform/scheduled-jobs/runs/0192f0c4-7b7a-7cc4-9a51-6f0a4c2b1d11", "", nil, 404, "platform.jobs.not_found"},
		{"unknown sub-resource", "GET", "/api/platform/scheduled-jobs/ai-eval/other", "", nil, 404, "common.error.not_found"},
		{"list runs", "GET", "/api/platform/scheduled-jobs/ai-eval/runs?page=3&per_page=500", "", nil, 200, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			}
			rec := serveJobs(t, f, tc.method, tc.path, tc.body, true)
			if rec.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
			if tc.wantCode != "" && !strings.Contains(rec.Body.String(), `"`+tc.wantCode+`"`) {
				t.Fatalf("want code %q in body, got %s", tc.wantCode, rec.Body.String())
			}
		})
	}
	if f.lastActor != "user-1" || f.lastTenant != "tenant-x" {
		t.Fatalf("actor/tenant must come from gateway headers, got %q/%q", f.lastActor, f.lastTenant)
	}
	if f.lastLimit != 100 || f.lastOffset != 200 {
		t.Fatalf("list runs must clamp per_page to 100 and derive offset, got limit=%d offset=%d", f.lastLimit, f.lastOffset)
	}
}

func TestScheduledJobListRunsEnvelope(t *testing.T) {
	rec := serveJobs(t, &fakeJobs{}, "GET", "/api/platform/scheduled-jobs/ai-eval/runs", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var env struct {
		Result struct {
			Items   []any `json:"items"`
			Page    int   `json:"page"`
			PerPage int   `json:"per_page"`
			Total   int   `json:"total"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Result.Items == nil || env.Result.Page != 1 || env.Result.PerPage != 20 {
		t.Fatalf("unexpected list envelope: %s", rec.Body.String())
	}
}

func TestScheduledJobWritesRequireAdmin(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/platform/scheduled-jobs/ai-eval/run", "{}"},
		{"PATCH", "/api/platform/scheduled-jobs/ai-eval", "{}"},
	} {
		rec := serveJobs(t, &fakeJobs{}, tc.method, tc.path, tc.body, false)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
			t.Fatalf("non-admin %s %s must be rejected, got %d", tc.method, tc.path, rec.Code)
		}
	}
}
