package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

var ErrJobAlreadyRunning = errors.New("job already running")

type ClaimedRun struct {
	Run domain.ScheduledJobRun
	Job domain.ScheduledJob
}

const runColumns = `id::text, job_code, trigger, scheduled_for, status, attempt, params, started_at,
	finished_at, deadline_at, error_code, error_message, result, triggered_by`

func scanRun(row rowScanner) (*domain.ScheduledJobRun, error) {
	var r domain.ScheduledJobRun
	var params, result []byte
	if err := row.Scan(&r.ID, &r.JobCode, &r.Trigger, &r.ScheduledFor, &r.Status, &r.Attempt, &params,
		&r.StartedAt, &r.FinishedAt, &r.DeadlineAt, &r.ErrorCode, &r.ErrorMessage, &result, &r.TriggeredBy); err != nil {
		return nil, err
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &r.Params)
	}
	if len(result) > 0 {
		r.Result = json.RawMessage(result)
	}
	return &r, nil
}

func deadline(job *domain.ScheduledJob, now time.Time) time.Time {
	// Retries share the deadline: attempts * timeout + backoff budget + 60s grace.
	budget := job.MaxAttempts*job.TimeoutSeconds + (job.MaxAttempts-1)*job.RetryBackoffSeconds*(1<<uint(job.MaxAttempts-1)) + 60
	return now.Add(time.Duration(budget) * time.Second)
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func insertRun(ctx context.Context, tx *sql.Tx, job *domain.ScheduledJob, trigger, status, actor string, params map[string]string, scheduledFor, now time.Time, errCode string) (*domain.ScheduledJobRun, error) {
	paramsJSON, _ := json.Marshal(params)
	var code *string
	var finished *time.Time
	if errCode != "" {
		code, finished = &errCode, &now
	}
	return scanRun(tx.QueryRowContext(ctx, `
		INSERT INTO plt_scheduled_job_runs (job_code, trigger, scheduled_for, status, params, started_at,
			deadline_at, triggered_by, error_code, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+runColumns,
		job.Code, trigger, scheduledFor, status, paramsJSON, now, deadline(job, now), actor, code, finished))
}

// ClaimDue locks due jobs (SKIP LOCKED so concurrent replicas partition the
// work), advances next_run_at past now (missed slots are not replayed), and
// inserts one RUNNING run per job, or a SKIPPED row when one is still active.
func (r *ScheduledJobRepository) ClaimDue(ctx context.Context, now time.Time, limit int) ([]ClaimedRun, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+jobColumns+` FROM plt_scheduled_jobs
		WHERE enabled AND retired_at IS NULL AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at LIMIT $2 FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	m := pgtype.NewMap()
	var due []domain.ScheduledJob
	for rows.Next() {
		j, err := scanJob(m, rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		due = append(due, *j)
	}
	rows.Close()
	var claimed []ClaimedRun
	for i := range due {
		job := &due[i]
		scheduledFor := *job.NextRunAt
		next, err := nextRunFor(job.CronExpr, job.Timezone, true, now)
		if err != nil {
			return nil, fmt.Errorf("next run for %s: %w", job.Code, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE plt_scheduled_jobs SET next_run_at = $2 WHERE code = $1`, job.Code, next); err != nil {
			return nil, err
		}
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM plt_scheduled_job_runs WHERE job_code=$1 AND status='RUNNING')`, job.Code).Scan(&active); err != nil {
			return nil, err
		}
		if active {
			if _, err := insertRun(ctx, tx, job, domain.JobTriggerSchedule, domain.JobStatusSkipped, "scheduler", nil, scheduledFor, now, "PREVIOUS_RUN_ACTIVE"); err != nil {
				return nil, err
			}
			continue
		}
		run, err := insertRun(ctx, tx, job, domain.JobTriggerSchedule, domain.JobStatusRunning, "scheduler", nil, scheduledFor, now, "")
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, ClaimedRun{Run: *run, Job: *job})
	}
	return claimed, tx.Commit()
}

func (r *ScheduledJobRepository) StartManual(ctx context.Context, code, actor string, params map[string]string, now time.Time) (*ClaimedRun, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := scanJob(pgtype.NewMap(), tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM plt_scheduled_jobs WHERE code = $1 FOR UPDATE`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	run, err := insertRun(ctx, tx, job, domain.JobTriggerManual, domain.JobStatusRunning, actor, params, now, now, "")
	if isUniqueViolation(err) {
		return nil, ErrJobAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	return &ClaimedRun{Run: *run, Job: *job}, tx.Commit()
}

// Finalize closes a RUNNING run, updates last_status and, on a state
// transition, writes the outbox event in the same transaction. Finalizing a
// run that is no longer RUNNING (e.g. already reaped) is a no-op.
func (r *ScheduledJobRepository) Finalize(ctx context.Context, runID string, out domain.RunOutcome, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := finalizeTx(ctx, tx, runID, out, now); err != nil {
		return err
	}
	return tx.Commit()
}

func finalizeTx(ctx context.Context, tx *sql.Tx, runID string, out domain.RunOutcome, now time.Time) error {
	var code, msg *string
	if out.ErrorCode != "" {
		c := out.ErrorCode
		code = &c
	}
	if out.ErrorMessage != "" {
		m := domain.Truncate(out.ErrorMessage, domain.MaxErrorMessageLen)
		msg = &m
	}
	var result []byte
	if len(out.Result) > 0 && len(out.Result) <= domain.MaxResultBytes && json.Valid(out.Result) {
		result = out.Result
	}
	var jobCode string
	err := tx.QueryRowContext(ctx, `
		UPDATE plt_scheduled_job_runs SET status = $2, attempt = $3, error_code = $4, error_message = $5,
			result = $6, finished_at = $7
		WHERE id = $1::uuid AND status = 'RUNNING'
		RETURNING job_code`, runID, out.Status, out.Attempt, code, msg, result, now).Scan(&jobCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	job, err := scanJob(pgtype.NewMap(), tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM plt_scheduled_jobs WHERE code = $1 FOR UPDATE`, jobCode))
	if err != nil {
		return err
	}
	prev := ""
	if job.LastStatus != nil {
		prev = *job.LastStatus
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plt_scheduled_jobs SET last_status = $2 WHERE code = $1`, jobCode, out.Status); err != nil {
		return err
	}
	subject, eventCode, ok := domain.Transition(prev, out.Status)
	if !ok {
		return nil
	}
	ev := domain.JobEvent{
		EventID: runID + ":" + eventCode, OccurredAt: now.UTC().Format(time.RFC3339), JobCode: job.Code,
		JobName: job.Name, RunID: runID, Status: out.Status, Attempt: out.Attempt, ErrorCode: out.ErrorCode,
		ErrorMessage: domain.Truncate(out.ErrorMessage, 300), TenantID: job.NotifyTenantID,
		UserIDs: job.NotifyUserIDs, GroupIDs: job.NotifyGroupIDs, RoleCodes: job.NotifyRoleCodes,
		Href: "/admin/jobs?tab=scheduled&run=" + runID, Locale: "vi-VN",
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO plt_outbox (subject, event_code, dedupe_key, payload) VALUES ($1,$2,$3,$4)
		ON CONFLICT (dedupe_key) DO NOTHING`, subject, eventCode, ev.EventID, payload)
	return err
}

func (r *ScheduledJobRepository) ReapAbandoned(ctx context.Context, now time.Time) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text, attempt FROM plt_scheduled_job_runs
		WHERE status = 'RUNNING' AND deadline_at < $1 FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return 0, err
	}
	type stale struct {
		id      string
		attempt int
	}
	var list []stale
	for rows.Next() {
		var s stale
		if err := rows.Scan(&s.id, &s.attempt); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, s)
	}
	rows.Close()
	for _, s := range list {
		if err := finalizeTx(ctx, tx, s.id, domain.RunOutcome{Status: domain.JobStatusTimedOut, Attempt: s.attempt,
			ErrorCode: "RUN_ABANDONED", ErrorMessage: "run exceeded its deadline without reporting a result"}, now); err != nil {
			return 0, err
		}
	}
	return len(list), tx.Commit()
}

func (r *ScheduledJobRepository) ListRuns(ctx context.Context, code, status string, limit, offset int) ([]domain.ScheduledJobRun, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM plt_scheduled_job_runs WHERE job_code = $1 AND ($2 = '' OR status = $2)`, code, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+runColumns+` FROM plt_scheduled_job_runs
		WHERE job_code = $1 AND ($2 = '' OR status = $2) ORDER BY scheduled_for DESC, id DESC LIMIT $3 OFFSET $4`, code, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.ScheduledJobRun{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		run.Result = nil // list view stays small; detail endpoint returns result
		out = append(out, *run)
	}
	return out, total, rows.Err()
}

func (r *ScheduledJobRepository) GetRun(ctx context.Context, id string) (*domain.ScheduledJobRun, error) {
	run, err := scanRun(r.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM plt_scheduled_job_runs WHERE id = $1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

func (r *ScheduledJobRepository) Cleanup(ctx context.Context, now time.Time) (int64, int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM plt_scheduled_job_runs WHERE status <> 'RUNNING' AND started_at < $1`, now.AddDate(0, 0, -90))
	if err != nil {
		return 0, 0, err
	}
	runs, _ := res.RowsAffected()
	res, err = r.db.ExecContext(ctx, `DELETE FROM plt_outbox WHERE published_at IS NOT NULL AND published_at < $1`, now.AddDate(0, 0, -7))
	if err != nil {
		return runs, 0, err
	}
	outbox, _ := res.RowsAffected()
	return runs, outbox, nil
}
