package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
)

func openSchedDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
}

var t0 = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

func TestSeedCatalogKeepsOperatorFields(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	ctx := context.Background()
	if err := repo.SeedCatalog(ctx, domain.JobCatalog, "tenant-a", t0); err != nil {
		t.Fatal(err)
	}
	job, err := repo.Get(ctx, "ai-eval")
	if err != nil {
		t.Fatal(err)
	}
	if job.CronExpr == nil || *job.CronExpr != "0 6 * * *" || job.NextRunAt == nil || job.NotifyTenantID != "tenant-a" {
		t.Fatalf("unexpected seeded job %+v", job)
	}
	cron := "15 7 * * *"
	if _, err := repo.UpdateConfig(ctx, "ai-eval", domain.JobConfigUpdate{CronExpr: &cron, Timezone: "Asia/Ho_Chi_Minh", Enabled: true,
		TimeoutSeconds: 900, MaxAttempts: 3, RetryBackoffSeconds: 30, NotifyRoleCodes: []string{"OPS"}, Version: job.Version}, "tenant-b", "u1", t0); err != nil {
		t.Fatal(err)
	}
	renamed := append([]domain.JobDefinition(nil), domain.JobCatalog...)
	renamed[0].Name = "Renamed eval"
	if err := repo.SeedCatalog(ctx, renamed, "tenant-a", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	job, _ = repo.Get(ctx, "ai-eval")
	if job.Name != "Renamed eval" || *job.CronExpr != "15 7 * * *" || job.TimeoutSeconds != 900 || job.NotifyTenantID != "tenant-b" {
		t.Fatalf("seed overwrote operator fields or missed code fields: %+v", job)
	}
}

func TestSeedCatalogRetiresRemovedJobs(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	ctx := context.Background()
	_ = repo.SeedCatalog(ctx, domain.JobCatalog, "tenant-a", t0)
	if err := repo.SeedCatalog(ctx, domain.JobCatalog[1:], "tenant-a", t0); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.Get(ctx, domain.JobCatalog[0].Code)
	if job.Enabled || job.RetiredAt == nil || job.NextRunAt != nil {
		t.Fatalf("removed job must be retired and disabled: %+v", job)
	}
}

func TestUpdateConfigVersionConflictAndAudit(t *testing.T) {
	db := openSchedDB(t)
	repo := NewScheduledJobRepository(db)
	ctx := context.Background()
	_ = repo.SeedCatalog(ctx, domain.JobCatalog, "tenant-a", t0)
	job, _ := repo.Get(ctx, "ai-eval")
	in := domain.JobConfigUpdate{CronExpr: job.CronExpr, Timezone: job.Timezone, Enabled: false, TimeoutSeconds: 600, MaxAttempts: 2, RetryBackoffSeconds: 120, Version: job.Version}
	updated, err := repo.UpdateConfig(ctx, "ai-eval", in, "tenant-a", "u1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || updated.NextRunAt != nil || updated.Version != job.Version+1 {
		t.Fatalf("disable must clear next_run_at and bump version: %+v", updated)
	}
	if _, err := repo.UpdateConfig(ctx, "ai-eval", in, "tenant-a", "u1", t0); !errors.Is(err, ardapostgres.ErrStaleVersion) {
		t.Fatalf("want ErrStaleVersion, got %v", err)
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM plt_audit_events WHERE entity='scheduled_job' AND entity_id='ai-eval' AND actor='u1'`).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 audit row, got %d", n)
	}
	if _, err := repo.UpdateConfig(ctx, "missing", in, "tenant-a", "u1", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
