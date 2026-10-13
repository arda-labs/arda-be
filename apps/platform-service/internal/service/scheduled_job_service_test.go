package service

import (
	"context"
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
)

func svcFixture(t *testing.T) *ScheduledJobService {
	db, s := schedulerFixture(t, `{"status":"succeeded"}`)
	return NewScheduledJobService(repository.NewScheduledJobRepository(db), s, s.exec, "tenant-a")
}

func TestServiceUpdateValidatesAndMapsErrors(t *testing.T) {
	svc := svcFixture(t)
	ctx := context.Background()
	job, err := svc.Get(ctx, "ai-eval")
	if err != nil {
		t.Fatal(err)
	}
	bad := "* * *"
	_, err = svc.Update(ctx, "ai-eval", domain.JobConfigUpdate{CronExpr: &bad, Timezone: "Asia/Ho_Chi_Minh", TimeoutSeconds: 600, MaxAttempts: 2, Version: job.Version}, "t", "u")
	if !errors.Is(err, domain.ErrInvalidCron) {
		t.Fatalf("want ErrInvalidCron, got %v", err)
	}
	good := "0 7 * * *"
	in := domain.JobConfigUpdate{CronExpr: &good, Timezone: "Asia/Ho_Chi_Minh", Enabled: true, TimeoutSeconds: 600, MaxAttempts: 2, Version: job.Version}
	if _, err := svc.Update(ctx, "ai-eval", in, "t", "u"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, "ai-eval", in, "t", "u"); !errors.Is(err, ErrJobVersionConflict) {
		t.Fatalf("want ErrJobVersionConflict, got %v", err)
	}
	if _, err := svc.Get(ctx, "missing"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("want ErrJobNotFound, got %v", err)
	}
}

func TestServiceRunNowRules(t *testing.T) {
	svc := svcFixture(t)
	ctx := context.Background()
	if _, err := svc.RunNow(ctx, "ai-eval", "u", map[string]string{"business_date": "2026-10-10"}); !errors.Is(err, ErrJobParamNotAllowed) {
		t.Fatalf("ai-eval accepts no params, got %v", err)
	}
	if _, err := svc.RunNow(ctx, "eod", "u", nil); !errors.Is(err, ErrJobTargetNotConfigured) {
		t.Fatalf("eod target is not configured in this fixture, got %v", err)
	}
	if _, err := svc.RunNow(ctx, "ai-eval", "u", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunNow(ctx, "ai-eval", "u", nil); !errors.Is(err, ErrJobAlreadyRunning) && err != nil {
		t.Fatalf("second run must be rejected or succeed after the first finished, got %v", err)
	}
	svc.scheduler.Wait()
}

func TestServiceRunNowUnknownJob(t *testing.T) {
	svc := svcFixture(t)
	if _, err := svc.RunNow(context.Background(), "missing", "u", nil); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("want ErrJobNotFound, got %v", err)
	}
}

func TestServiceRunNowSchedulerBusyPassesThrough(t *testing.T) {
	svc := svcFixture(t)
	fillSlots(svc.scheduler)
	run, err := svc.RunNow(context.Background(), "ai-eval", "u", nil)
	if !errors.Is(err, ErrSchedulerBusy) || run != nil {
		t.Fatalf("want ErrSchedulerBusy, got run=%+v err=%v", run, err)
	}
}

func TestServiceRunNowAfterShutdownIsBusy(t *testing.T) {
	svc := svcFixture(t)
	if err := svc.scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunNow(context.Background(), "ai-eval", "u", nil); !errors.Is(err, ErrSchedulerBusy) {
		t.Fatalf("want ErrSchedulerBusy, got %v", err)
	}
}

func TestServiceRunNowRejectsRetiredJob(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"succeeded"}`)
	svc := NewScheduledJobService(repository.NewScheduledJobRepository(db), s, s.exec, "tenant-a")
	if _, err := db.Exec(`UPDATE plt_scheduled_jobs SET retired_at = now() WHERE code='ai-eval'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunNow(context.Background(), "ai-eval", "u", nil); !errors.Is(err, ErrJobRetired) {
		t.Fatalf("want ErrJobRetired, got %v", err)
	}
	var runs int
	if err := db.QueryRow(`SELECT count(*) FROM plt_scheduled_job_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("no run row may be created for a retired job, got %d", runs)
	}
}

func TestServiceRunNowAllowsDisabledJob(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"succeeded"}`)
	svc := NewScheduledJobService(repository.NewScheduledJobRepository(db), s, s.exec, "tenant-a")
	if _, err := db.Exec(`UPDATE plt_scheduled_jobs SET enabled = false WHERE code='ai-eval'`); err != nil {
		t.Fatal(err)
	}
	run, err := svc.RunNow(context.Background(), "ai-eval", "u", nil)
	if err != nil {
		t.Fatalf("disabled job must still run manually, got %v", err)
	}
	if run.Trigger != domain.JobTriggerManual || run.TriggeredBy != "u" {
		t.Fatalf("unexpected run %+v", run)
	}
	s.Wait()
}

func TestServiceListMarksTargetConfigured(t *testing.T) {
	svc := svcFixture(t)
	jobs, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Code == "ai-eval" && !j.TargetConfigured {
			t.Fatal("ai-eval target is configured in fixture")
		}
		if j.Code == "eod" && j.TargetConfigured {
			t.Fatal("eod target is not configured in fixture")
		}
	}
}

func TestServiceListFillsLastRun(t *testing.T) {
	svc := svcFixture(t)
	ctx := context.Background()
	run, err := svc.RunNow(ctx, "ai-eval", "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.scheduler.Wait()
	jobs, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Code == "ai-eval" && (j.LastRun == nil || j.LastRun.ID != run.ID) {
			t.Fatalf("ai-eval last run = %+v, want %s", j.LastRun, run.ID)
		}
		if j.Code == "eod" && j.LastRun != nil {
			t.Fatalf("eod has no runs, got %+v", j.LastRun)
		}
	}
	got, err := svc.GetRun(ctx, run.ID)
	if err != nil || got.ID != run.ID {
		t.Fatalf("GetRun: %+v %v", got, err)
	}
}
