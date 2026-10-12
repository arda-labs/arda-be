package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	_, _ = db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = now() - interval '1 minute' WHERE code='ai-eval'`)
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

func fillSlots(s *JobScheduler) {
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
}

func TestRunNowBusyWhenSlotsFull(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"succeeded"}`)
	_, _ = db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = now() - interval '1 minute' WHERE code='ai-eval'`)
	fillSlots(s)
	run, err := s.RunNow(context.Background(), "ai-eval", "u1", nil)
	if !errors.Is(err, ErrSchedulerBusy) || run != nil {
		t.Fatalf("want ErrSchedulerBusy, got run=%+v err=%v", run, err)
	}
	if n := s.Tick(context.Background()); n != 0 {
		t.Fatalf("tick with no free slot must claim nothing, got %d", n)
	}
	var runs int
	if err := db.QueryRow(`SELECT count(*) FROM plt_scheduled_job_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("no run row may be created while busy, got %d", runs)
	}
}

func TestShutdownRefusesNewRuns(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"succeeded"}`)
	_, _ = db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = now() - interval '1 minute' WHERE code='ai-eval'`)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown with nothing in flight: %v", err)
	}
	if _, err := s.RunNow(context.Background(), "ai-eval", "u1", nil); !errors.Is(err, ErrSchedulerBusy) {
		t.Fatalf("want ErrSchedulerBusy after shutdown, got %v", err)
	}
	if n := s.Tick(context.Background()); n != 0 {
		t.Fatalf("tick after shutdown must claim nothing, got %d", n)
	}
	var runs int
	_ = db.QueryRow(`SELECT count(*) FROM plt_scheduled_job_runs`).Scan(&runs)
	if runs != 0 {
		t.Fatalf("no run row may be created after shutdown, got %d", runs)
	}
}

// fakeRunStore is an in-memory JobRunStore for non-DB scheduler behaviour.
type fakeRunStore struct {
	claimErr      error
	finalizeFails int32 // number of Finalize calls that fail before success
	finalizeCalls atomic.Int32
	release       chan struct{} // if set, Finalize blocks until closed
	claimCalls    atomic.Int32
}

func (f *fakeRunStore) ClaimDue(context.Context, time.Time, int) ([]repository.ClaimedRun, error) {
	f.claimCalls.Add(1)
	return nil, f.claimErr
}

func (f *fakeRunStore) StartManual(_ context.Context, code, actor string, _ map[string]string, _ time.Time) (*repository.ClaimedRun, error) {
	// OwnerService has no configured target, so Execute fails fast without HTTP.
	return &repository.ClaimedRun{
		Run: domain.ScheduledJobRun{ID: "run-" + code, JobCode: code, Trigger: domain.JobTriggerManual, Status: domain.JobStatusRunning},
		Job: domain.ScheduledJob{Code: code, OwnerService: "nowhere", MaxAttempts: 1, TimeoutSeconds: 10},
	}, nil
}

func (f *fakeRunStore) Finalize(ctx context.Context, _ string, _ domain.RunOutcome, _ time.Time) error {
	n := f.finalizeCalls.Add(1)
	if f.release != nil {
		<-f.release
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("finalize ctx must carry a timeout")
	}
	if n <= f.finalizeFails {
		return errors.New("db blip")
	}
	return nil
}

func (f *fakeRunStore) ReapAbandoned(context.Context, time.Time) (int, error) { return 0, nil }

func fakeScheduler(store *fakeRunStore) *JobScheduler {
	ex := NewJobExecutor(map[string]string{}, execSecret, http.DefaultClient)
	s := NewJobScheduler(store, ex, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.finalizeBackoff = []time.Duration{0, 0}
	return s
}

func metrics(s *JobScheduler) string {
	var b strings.Builder
	s.RenderMetrics(&b)
	return b.String()
}

func TestFinalizeRetriesTransientErrors(t *testing.T) {
	store := &fakeRunStore{finalizeFails: 2}
	s := fakeScheduler(store)
	if _, err := s.RunNow(context.Background(), "ai-eval", "u1", nil); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	m := metrics(s)
	if store.finalizeCalls.Load() != 3 || !strings.Contains(m, `arda_platform_scheduled_job_runs_total{status="FAILED"} 1`) ||
		!strings.Contains(m, `arda_platform_scheduled_job_runs_total{status="finalize_error"} 0`) {
		t.Fatalf("calls=%d metrics:\n%s", store.finalizeCalls.Load(), m)
	}
}

func TestFinalizeGivesUpAfterThreeAttempts(t *testing.T) {
	store := &fakeRunStore{finalizeFails: 100}
	s := fakeScheduler(store)
	if _, err := s.RunNow(context.Background(), "ai-eval", "u1", nil); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	m := metrics(s)
	if store.finalizeCalls.Load() != 3 || !strings.Contains(m, `arda_platform_scheduled_job_runs_total{status="finalize_error"} 1`) ||
		!strings.Contains(m, `arda_platform_scheduled_job_runs_total{status="FAILED"} 0`) {
		t.Fatalf("calls=%d metrics:\n%s", store.finalizeCalls.Load(), m)
	}
}

func TestShutdownWaitsForInFlightUntilDeadline(t *testing.T) {
	store := &fakeRunStore{release: make(chan struct{})}
	s := fakeScheduler(store)
	if _, err := s.RunNow(context.Background(), "ai-eval", "u1", nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded while run in flight, got %v", err)
	}
	close(store.release)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown after run finished: %v", err)
	}
}

func TestClaimErrorCountedAndGaugeNotAdvanced(t *testing.T) {
	s := fakeScheduler(&fakeRunStore{claimErr: errors.New("db down")})
	if n := s.Tick(context.Background()); n != 0 {
		t.Fatalf("got %d", n)
	}
	m := metrics(s)
	if !strings.Contains(m, "arda_platform_scheduler_claim_errors_total 1") ||
		!strings.Contains(m, "arda_platform_scheduler_last_tick_timestamp_seconds 0") {
		t.Fatalf("metrics:\n%s", m)
	}
	if len(s.slots) != 0 {
		t.Fatalf("reserved slots must be released on claim error, %d held", len(s.slots))
	}
	for _, st := range []string{"SUCCEEDED", "FAILED", "TIMED_OUT", "finalize_error"} {
		if !strings.Contains(m, `arda_platform_scheduled_job_runs_total{status="`+st+`"} 0`) {
			t.Fatalf("series %s not pre-registered:\n%s", st, m)
		}
	}
}

func TestTickAdvancesGaugeWhenSaturated(t *testing.T) {
	store := &fakeRunStore{}
	s := fakeScheduler(store)
	fillSlots(s)
	s.lastTick.Store(1000)
	if n := s.Tick(context.Background()); n != 0 {
		t.Fatalf("saturated tick dispatched %d", n)
	}
	if got := s.lastTick.Load(); got <= 1000 {
		t.Fatalf("heartbeat must advance while saturated, got %d", got)
	}
	if c := store.claimCalls.Load(); c != 0 {
		t.Fatalf("saturated tick must not call ClaimDue, got %d calls", c)
	}
}

func TestTickAfterShutdownKeepsGauge(t *testing.T) {
	store := &fakeRunStore{}
	s := fakeScheduler(store)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.lastTick.Store(1000)
	if n := s.Tick(context.Background()); n != 0 {
		t.Fatalf("got %d", n)
	}
	if got := s.lastTick.Load(); got != 1000 {
		t.Fatalf("heartbeat must not advance after shutdown, got %d", got)
	}
	if c := store.claimCalls.Load(); c != 0 {
		t.Fatalf("tick after shutdown must not call ClaimDue, got %d calls", c)
	}
}

func TestTickReleasesUnusedSlots(t *testing.T) {
	s := fakeScheduler(&fakeRunStore{})
	if n := s.Tick(context.Background()); n != 0 {
		t.Fatalf("got %d", n)
	}
	if len(s.slots) != 0 {
		t.Fatalf("unused reservations must be released, %d held", len(s.slots))
	}
	s.Wait()
	if strings.Contains(metrics(s), "arda_platform_scheduler_last_tick_timestamp_seconds 0\n") {
		t.Fatal("gauge must advance after a successful claim")
	}
}

// With JOB_TARGET_URLS unset the scheduler still runs: a due job whose owner
// has no target is finalized FAILED/JOB_TARGET_NOT_CONFIGURED (and raises the
// failure event) instead of panicking or stalling the tick.
func TestTickFailsRunWhenTargetNotConfigured(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"succeeded"}`)
	s.exec = NewJobExecutor(map[string]string{}, execSecret, nil)
	_, _ = db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = now() - interval '1 minute' WHERE code='ai-eval'`)
	if n := s.Tick(context.Background()); n != 1 {
		t.Fatalf("want 1 dispatched, got %d", n)
	}
	s.Wait()
	var status, code string
	if err := db.QueryRow(`SELECT status, error_code FROM plt_scheduled_job_runs WHERE job_code='ai-eval'`).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != domain.JobStatusFailed || code != "JOB_TARGET_NOT_CONFIGURED" {
		t.Fatalf("status=%s code=%s", status, code)
	}
	var events int
	_ = db.QueryRow(`SELECT count(*) FROM plt_outbox WHERE subject=$1`, domain.SubjectJobFailed).Scan(&events)
	if events != 1 {
		t.Fatalf("want 1 failure event, got %d", events)
	}
	if n := s.Tick(context.Background()); n != 0 {
		t.Fatalf("job must be rescheduled, not re-claimed on the next tick; got %d", n)
	}
}
