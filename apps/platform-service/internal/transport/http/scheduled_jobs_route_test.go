package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/handler"
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

func TestNewRouterWithoutScheduledJobs(t *testing.T) {
	router := NewRouter(handler.NewPlatformHandler(nil, nil), handler.NewCalendarHandler(nil), nil, nil, WithScheduledJobs(nil))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/platform/scheduled-jobs", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unwired scheduled jobs must 404, got %d", rec.Code)
	}
}
