package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

type ScheduledJobRepository struct{ db *sql.DB }

func NewScheduledJobRepository(db *sql.DB) *ScheduledJobRepository {
	return &ScheduledJobRepository{db: db}
}

const jobColumns = `code, name, owner_service, path, cron_expr, timezone, enabled, timeout_seconds,
	max_attempts, retry_backoff_seconds, notify_tenant_id, notify_user_ids, notify_group_ids,
	notify_role_codes, next_run_at, last_status, retired_at, version, updated_at, updated_by`

type rowScanner interface{ Scan(dest ...any) error }

// scanJob reads one row. pgtype.FlatArray is not an sql.Scanner, so TEXT[]
// columns go through m.SQLScanner to work with the pgx stdlib driver. A
// pgtype.Map memoizes plans and is not safe for concurrent use, so callers
// build one per repository call and reuse it across that call's rows.
func scanJob(m *pgtype.Map, row rowScanner) (*domain.ScheduledJob, error) {
	var j domain.ScheduledJob
	var users, groups, roles []string
	if err := row.Scan(&j.Code, &j.Name, &j.OwnerService, &j.Path, &j.CronExpr, &j.Timezone, &j.Enabled,
		&j.TimeoutSeconds, &j.MaxAttempts, &j.RetryBackoffSeconds, &j.NotifyTenantID, m.SQLScanner(&users),
		m.SQLScanner(&groups), m.SQLScanner(&roles), &j.NextRunAt, &j.LastStatus, &j.RetiredAt, &j.Version, &j.UpdatedAt, &j.UpdatedBy); err != nil {
		return nil, err
	}
	j.NotifyUserIDs, j.NotifyGroupIDs, j.NotifyRoleCodes = nonNil(users), nonNil(groups), nonNil(roles)
	return &j, nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func nextRunFor(cronExpr *string, tz string, enabled bool, now time.Time) (*time.Time, error) {
	if !enabled || cronExpr == nil || *cronExpr == "" {
		return nil, nil
	}
	next, err := domain.NextRun(*cronExpr, tz, now)
	if err != nil {
		return nil, err
	}
	return &next, nil
}

// SeedCatalog upserts code-owned columns only. Operator-owned columns get
// defaults on insert and are never touched again. Jobs missing from defs are
// retired: only retired_at is set (scheduling skips retired rows), so enabled
// and next_run_at survive an un-retire when a newer catalog is seeded again
// (e.g. an old pod seeding an older catalog during a rolling deploy).
func (r *ScheduledJobRepository) SeedCatalog(ctx context.Context, defs []domain.JobDefinition, defaultTenant string, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	codes := make([]string, 0, len(defs))
	for _, d := range defs {
		codes = append(codes, d.Code)
		var cron *string
		if d.DefaultCron != "" {
			c := d.DefaultCron
			cron = &c
		}
		next, err := nextRunFor(cron, domain.DefaultTimezone, true, now)
		if err != nil {
			return fmt.Errorf("seed %s: %w", d.Code, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO plt_scheduled_jobs (code, name, owner_service, path, cron_expr, timeout_seconds,
				max_attempts, retry_backoff_seconds, notify_tenant_id, next_run_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (code) DO UPDATE SET
				name = EXCLUDED.name, owner_service = EXCLUDED.owner_service, path = EXCLUDED.path,
				retired_at = NULL, updated_at = now()
			WHERE plt_scheduled_jobs.name IS DISTINCT FROM EXCLUDED.name
			   OR plt_scheduled_jobs.owner_service IS DISTINCT FROM EXCLUDED.owner_service
			   OR plt_scheduled_jobs.path IS DISTINCT FROM EXCLUDED.path
			   OR plt_scheduled_jobs.retired_at IS NOT NULL`,
			d.Code, d.Name, d.OwnerService, d.Path, cron, d.DefaultTimeoutSeconds, d.DefaultMaxAttempts,
			d.DefaultBackoffSeconds, defaultTenant, next); err != nil {
			return fmt.Errorf("seed %s: %w", d.Code, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE plt_scheduled_jobs SET retired_at = $2, updated_at = now()
		WHERE retired_at IS NULL AND NOT (code = ANY($1))`, codes, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ScheduledJobRepository) List(ctx context.Context) ([]domain.ScheduledJob, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM plt_scheduled_jobs ORDER BY retired_at NULLS FIRST, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := pgtype.NewMap()
	out := []domain.ScheduledJob{}
	for rows.Next() {
		j, err := scanJob(m, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *ScheduledJobRepository) Get(ctx context.Context, code string) (*domain.ScheduledJob, error) {
	j, err := scanJob(pgtype.NewMap(), r.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM plt_scheduled_jobs WHERE code = $1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

// UpdateConfig writes operator-owned columns, recomputes next_run_at and
// records an audit row in the same transaction.
func (r *ScheduledJobRepository) UpdateConfig(ctx context.Context, code string, in domain.JobConfigUpdate, tenantID, actor string, now time.Time) (*domain.ScheduledJob, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	m := pgtype.NewMap()
	before, err := scanJob(m, tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM plt_scheduled_jobs WHERE code = $1 FOR UPDATE`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var cron *string
	if c := in.Cron(); c != "" {
		cron = &c
	}
	enabled := in.Enabled && before.RetiredAt == nil
	next, err := nextRunFor(cron, in.Timezone, enabled, now)
	if err != nil {
		return nil, err
	}
	if _, err := ardapostgres.UpdateVersion(tx.QueryRowContext(ctx, `
		UPDATE plt_scheduled_jobs SET cron_expr = $3, timezone = $4, enabled = $5, timeout_seconds = $6,
			max_attempts = $7, retry_backoff_seconds = $8, notify_tenant_id = $9, notify_user_ids = $10,
			notify_group_ids = $11, notify_role_codes = $12, next_run_at = $13,
			updated_at = now(), updated_by = $14, version = version + 1
		WHERE code = $1 AND version = $2
		RETURNING version`,
		code, in.Version, cron, in.Timezone, enabled, in.TimeoutSeconds, in.MaxAttempts, in.RetryBackoffSeconds,
		tenantID, nonNil(in.NotifyUserIDs), nonNil(in.NotifyGroupIDs), nonNil(in.NotifyRoleCodes), next, actor)); err != nil {
		return nil, err
	}
	after, err := scanJob(m, tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM plt_scheduled_jobs WHERE code = $1`, code))
	if err != nil {
		return nil, err
	}
	oldJSON, _ := json.Marshal(before)
	newJSON, _ := json.Marshal(after)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plt_audit_events (tenant_id, entity, entity_id, action, old_value, new_value, actor)
		VALUES ($1,'scheduled_job',$2,'update',$3,$4,$5)`, tenantID, code, oldJSON, newJSON, actor); err != nil {
		return nil, err
	}
	return after, tx.Commit()
}
