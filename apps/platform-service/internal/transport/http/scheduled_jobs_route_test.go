package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/handler"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

type stubJobs struct{}

func (stubJobs) List(context.Context) ([]domain.ScheduledJob, error) { return nil, nil }
func (stubJobs) Get(context.Context, string) (*domain.ScheduledJob, error) {
	return &domain.ScheduledJob{}, nil
}
func (stubJobs) Update(context.Context, string, domain.JobConfigUpdate, string, string) (*domain.ScheduledJob, error) {
	return &domain.ScheduledJob{}, nil
}
func (stubJobs) RunNow(context.Context, string, string, map[string]string) (*domain.ScheduledJobRun, error) {
	return &domain.ScheduledJobRun{}, nil
}
func (stubJobs) Preview(string, string) ([]time.Time, error) { return nil, nil }
func (stubJobs) ListRuns(context.Context, string, string, int, int) ([]domain.ScheduledJobRun, int, error) {
	return nil, 0, nil
}
func (stubJobs) GetRun(context.Context, string) (*domain.ScheduledJobRun, error) {
	return &domain.ScheduledJobRun{}, nil
}

// The scheduled-jobs routes must coexist with every existing platform route
// (ServeMux panics on conflicting patterns at registration time).
func TestNewRouterMountsScheduledJobs(t *testing.T) {
	router := NewRouter(
		handler.NewPlatformHandler(nil, nil),
		handler.NewCalendarHandler(nil),
		nil,
		nil,
		WithScheduledJobs(handler.NewScheduledJobHandler(stubJobs{})),
	)
	for _, path := range []string{
		"/api/platform/scheduled-jobs",
		"/api/platform/scheduled-jobs/ai-eval",
		"/api/platform/scheduled-jobs/ai-eval/runs",
		"/api/platform/scheduled-jobs/runs/0192f0c4-7b7a-7cc4-9a51-6f0a4c2b1d11",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200: %s", path, rec.Code, rec.Body.String())
		}
	}
}

// A nil handler is a wiring bug: the option must fail loudly at construction
// instead of letting the routes silently 404.
func TestRouterOptionsRejectNilHandlers(t *testing.T) {
	for name, build := range map[string]func(){
		"WithScheduledJobs": func() { WithScheduledJobs(nil) },
		"WithInternalJobs":  func() { WithInternalJobs(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s(nil) must panic", name)
				}
			}()
			build()
		})
	}
}

type stubEOD struct{ called bool }

func (s *stubEOD) RunSystem(context.Context, string) (*service.RunResult, error) {
	s.called = true
	return &service.RunResult{}, nil
}

type stubCleaner struct{}

func (stubCleaner) Cleanup(context.Context, time.Time) (int64, int64, error) { return 0, 0, nil }

// Mirrors main's router: both scheduled-job features mounted side by side.
// The admin API answers, and the internal job targets are routed behind the
// platform-service signature (unsigned → 401, wrong source → 403, signed → 200).
func TestNewRouterMountsScheduledAndInternalJobs(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", internalAITestSecret)
	eod := &stubEOD{}
	router := NewRouter(
		handler.NewPlatformHandler(nil, nil),
		handler.NewCalendarHandler(nil),
		nil,
		nil,
		WithScheduledJobs(handler.NewScheduledJobHandler(stubJobs{})),
		WithInternalJobs(handler.NewInternalJobsHandler(eod, stubCleaner{})),
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/platform/scheduled-jobs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/platform/scheduled-jobs = %d, want 200", rec.Code)
	}

	for _, path := range []string{"/internal/jobs/eod", "/internal/jobs/platform-runs-cleanup"} {
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("unsigned POST %s = %d, want 401", path, rec.Code)
		}

		wrong := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		if err := identity.SignRequest(wrong, internalAITestSecret, "ai-service", "platform-service", time.Now(), time.Minute); err != nil {
			t.Fatalf("sign: %v", err)
		}
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, wrong)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s from ai-service = %d, want 403", path, rec.Code)
		}
	}

	signed := httptest.NewRequest(http.MethodPost, "/internal/jobs/eod", strings.NewReader(`{}`))
	if err := identity.SignRequest(signed, internalAITestSecret, "platform-service", "platform-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, signed)
	if rec.Code != http.StatusOK || !eod.called || !strings.Contains(rec.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("signed POST /internal/jobs/eod = %d called=%v body=%s", rec.Code, eod.called, rec.Body.String())
	}
}
