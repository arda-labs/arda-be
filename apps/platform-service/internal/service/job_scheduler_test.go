package service

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
)

func schedulerFixture(t *testing.T, body string) (*sql.DB, *JobScheduler) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	repo := repository.NewScheduledJobRepository(db)
	if err := repo.SeedCatalog(context.Background(), domain.JobCatalog, "tenant-a", time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(identity.RequireServiceAuth(execSecret, "ai-service", identity.AllowedSources("platform-service"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })))
	t.Cleanup(srv.Close)
	ex := NewJobExecutor(map[string]string{"ai-service": srv.URL}, execSecret, srv.Client())
	return db, NewJobScheduler(repo, ex, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestTickRunsDueJobAndRecordsOutcome(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"failed","error_code":"EVAL_FAILED","error_message":"0/12"}`)
	_, _ = db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = now() - interval '1 second' WHERE code='ai-eval'`)
	if n := s.Tick(context.Background()); n != 1 {
		t.Fatalf("want 1 dispatched, got %d", n)
	}
	s.Wait()
	var status string
	_ = db.QueryRow(`SELECT status FROM plt_scheduled_job_runs WHERE job_code='ai-eval'`).Scan(&status)
	var events int
	_ = db.QueryRow(`SELECT count(*) FROM plt_outbox WHERE subject=$1`, domain.SubjectJobFailed).Scan(&events)
	if status != domain.JobStatusFailed || events != 1 {
		t.Fatalf("status=%s events=%d", status, events)
	}
	var buf strings.Builder
	s.RenderMetrics(&buf)
	if !strings.Contains(buf.String(), "arda_platform_scheduler_last_tick_timestamp_seconds") {
		t.Fatalf("missing gauge: %s", buf.String())
	}
}

func TestRunNowReturnsRunningRun(t *testing.T) {
	_, s := schedulerFixture(t, `{"status":"succeeded"}`)
	run, err := s.RunNow(context.Background(), "ai-eval", "u1", nil)
	if err != nil || run.Status != domain.JobStatusRunning || run.Trigger != domain.JobTriggerManual {
		t.Fatalf("unexpected %+v err=%v", run, err)
	}
	s.Wait()
}
