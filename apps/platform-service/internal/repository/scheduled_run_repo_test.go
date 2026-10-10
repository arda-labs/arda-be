package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

func seedDue(t *testing.T, repo *ScheduledJobRepository) {
	t.Helper()
	ctx := context.Background()
	if err := repo.SeedCatalog(ctx, domain.JobCatalog, "tenant-a", t0); err != nil {
		t.Fatal(err)
	}
	// force ai-eval due now
	if _, err := repo.db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = $1 WHERE code = 'ai-eval'`, t0); err != nil {
		t.Fatal(err)
	}
}

func TestClaimDueConcurrentReplicas(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	var wg sync.WaitGroup
	results := make([][]ClaimedRun, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := repo.ClaimDue(context.Background(), t0.Add(time.Second), 10)
			if err != nil {
				t.Error(err)
			}
			results[i] = got
		}(i)
	}
	wg.Wait()
	total := 0
	for _, r := range results {
		total += len(r)
	}
	var runs int
	_ = db.QueryRow(`SELECT count(*) FROM plt_scheduled_job_runs WHERE job_code='ai-eval'`).Scan(&runs)
	if total != 1 || runs != 1 {
		t.Fatalf("want exactly one claimed run, got claimed=%d rows=%d", total, runs)
	}
	job, _ := repo.Get(context.Background(), "ai-eval")
	if job.NextRunAt == nil || !job.NextRunAt.After(t0.Add(time.Second)) {
		t.Fatalf("next_run_at must advance past now, got %v", job.NextRunAt)
	}
}

func TestClaimDueSkipsWhilePreviousRunActive(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	ctx := context.Background()
	first, _ := repo.ClaimDue(ctx, t0.Add(time.Second), 10)
	if len(first) != 1 {
		t.Fatalf("want 1 claim, got %d", len(first))
	}
	_, _ = db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = $1 WHERE code='ai-eval'`, t0.Add(time.Minute))
	second, err := repo.ClaimDue(ctx, t0.Add(2*time.Minute), 10)
	if err != nil || len(second) != 0 {
		t.Fatalf("want 0 claims while running, got %d err=%v", len(second), err)
	}
	var skipped int
	_ = db.QueryRow(`SELECT count(*) FROM plt_scheduled_job_runs WHERE job_code='ai-eval' AND status='SKIPPED' AND error_code='PREVIOUS_RUN_ACTIVE'`).Scan(&skipped)
	if skipped != 1 {
		t.Fatalf("want 1 skipped row, got %d", skipped)
	}
}

func TestStartManualRejectsWhileRunning(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	ctx := context.Background()
	if _, err := repo.StartManual(ctx, "eod", "u1", map[string]string{"business_date": "2026-10-10"}, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.StartManual(ctx, "eod", "u1", nil, t0); !errors.Is(err, ErrJobAlreadyRunning) {
		t.Fatalf("want ErrJobAlreadyRunning, got %v", err)
	}
	if _, err := repo.StartManual(ctx, "nope", "u1", nil, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func outboxCount(t *testing.T, repo *ScheduledJobRepository, subject string) int {
	var n int
	_ = repo.db.QueryRow(`SELECT count(*) FROM plt_outbox WHERE subject = $1`, subject).Scan(&n)
	return n
}

func runOnce(t *testing.T, repo *ScheduledJobRepository, at time.Time, status string) {
	t.Helper()
	ctx := context.Background()
	c, err := repo.StartManual(ctx, "ai-eval", "u1", nil, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Finalize(ctx, c.Run.ID, domain.RunOutcome{Status: status, Attempt: 1, ErrorCode: "X", ErrorMessage: "boom"}, at); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeEmitsOnlyOnTransition(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	runOnce(t, repo, t0, domain.JobStatusFailed)                     // first ever failure -> failed
	runOnce(t, repo, t0.Add(time.Hour), domain.JobStatusFailed)      // repeat -> nothing
	runOnce(t, repo, t0.Add(2*time.Hour), domain.JobStatusTimedOut)  // still failing -> nothing
	runOnce(t, repo, t0.Add(3*time.Hour), domain.JobStatusSucceeded) // recovered
	runOnce(t, repo, t0.Add(4*time.Hour), domain.JobStatusSucceeded) // nothing
	if f, r := outboxCount(t, repo, domain.SubjectJobFailed), outboxCount(t, repo, domain.SubjectJobRecovered); f != 1 || r != 1 {
		t.Fatalf("want 1 failed + 1 recovered event, got %d + %d", f, r)
	}
	var payload []byte
	_ = db.QueryRow(`SELECT payload FROM plt_outbox WHERE subject=$1`, domain.SubjectJobFailed).Scan(&payload)
	var ev domain.JobEvent
	_ = json.Unmarshal(payload, &ev)
	if ev.JobCode != "ai-eval" || ev.TenantID != "tenant-a" || len(ev.RoleCodes) != 1 || ev.RoleCodes[0] != "SUPER_ADMIN" || ev.Href == "" || ev.EventID == "" {
		t.Fatalf("bad event payload %+v", ev)
	}
}

func TestReapAbandonedTimesOutPastDeadline(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	c, _ := repo.StartManual(context.Background(), "ai-eval", "u1", nil, t0)
	n, err := repo.ReapAbandoned(context.Background(), c.Run.DeadlineAt.Add(time.Second))
	if err != nil || n != 1 {
		t.Fatalf("want 1 reaped, got %d err=%v", n, err)
	}
	run, _ := repo.GetRun(context.Background(), c.Run.ID)
	if run.Status != domain.JobStatusTimedOut || run.ErrorCode == nil || *run.ErrorCode != "RUN_ABANDONED" {
		t.Fatalf("unexpected reaped run %+v", run)
	}
	if outboxCount(t, repo, domain.SubjectJobFailed) != 1 {
		t.Fatal("reaped run must emit failed event")
	}
}

func TestCleanupRemovesOldRows(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	runOnce(t, repo, t0, domain.JobStatusFailed)
	_, _ = db.Exec(`UPDATE plt_scheduled_job_runs SET started_at = $1`, t0.AddDate(0, 0, -91))
	_, _ = db.Exec(`UPDATE plt_outbox SET published_at = $1`, t0.AddDate(0, 0, -8))
	runs, outbox, err := repo.Cleanup(context.Background(), t0)
	if err != nil || runs != 1 || outbox != 1 {
		t.Fatalf("want 1/1 deleted, got %d/%d err=%v", runs, outbox, err)
	}
}

func TestClaimDuePoisonJobDoesNotBlockOthers(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE plt_scheduled_jobs SET next_run_at = $1, timezone = 'Not/AZone' WHERE code = 'platform-runs-cleanup'`, t0); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimDue(ctx, t0.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("poison job must not fail the batch: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Job.Code != "ai-eval" {
		t.Fatalf("want ai-eval claimed, got %+v", claimed)
	}
	bad, err := repo.Get(ctx, "platform-runs-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	if bad.NextRunAt != nil {
		t.Fatalf("poison job must stop firing, next_run_at=%v", bad.NextRunAt)
	}
	var n int
	var msg *string
	_ = db.QueryRow(`SELECT count(*), max(error_message) FROM plt_scheduled_job_runs
		WHERE job_code='platform-runs-cleanup' AND status='SKIPPED' AND error_code='SCHEDULE_ERROR'`).Scan(&n, &msg)
	if n != 1 || msg == nil || *msg == "" {
		t.Fatalf("want 1 SKIPPED/SCHEDULE_ERROR run with message, got %d msg=%v", n, msg)
	}
}

func TestFinalizeRejectsNonTerminalStatus(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	seedDue(t, repo)
	ctx := context.Background()
	c, err := repo.StartManual(ctx, "ai-eval", "u1", nil, t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{domain.JobStatusRunning, domain.JobStatusSkipped, ""} {
		if err := repo.Finalize(ctx, c.Run.ID, domain.RunOutcome{Status: s, Attempt: 1}, t0); err == nil {
			t.Fatalf("status %q must be rejected", s)
		}
	}
	run, _ := repo.GetRun(ctx, c.Run.ID)
	if run.Status != domain.JobStatusRunning {
		t.Fatalf("run must stay RUNNING, got %s", run.Status)
	}
}
