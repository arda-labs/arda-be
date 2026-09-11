package service

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// EODService runs the COB job sequence: ordered, idempotent per business
// date, with checkpoint rows in plt_job_runs (service-boundary-design §6).
type EODService struct {
	db     *sql.DB
	client *http.Client
	logger *slog.Logger
}

func NewEODService(db *sql.DB, logger *slog.Logger) *EODService {
	return &EODService{db: db, client: &http.Client{Timeout: 5 * time.Minute}, logger: logger}
}

// JobDefinition is one EOD step.
type JobDefinition struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Sequence  int    `json:"sequence"`
	Endpoint  string `json:"endpoint"`
	IsEnabled bool   `json:"is_enabled"`
}

// RunResult summarizes one COB execution.
type RunResult struct {
	BusinessDate string       `json:"business_date"`
	Steps        []StepResult `json:"steps"`
}

// StepResult is one job step outcome.
type StepResult struct {
	JobCode string `json:"job_code"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// SeedJobs upserts the default COB sequence (loan accrual -> provision).
func (s *EODService) SeedJobs(ctx context.Context, tenantID string) error {
	jobs := []JobDefinition{
		{Code: "LNM_ACCRUAL_DAILY", Name: "Tính lãi cho vay (EOD)", Sequence: 10, Endpoint: "http://loan-service:8097/internal/jobs/accrual-daily", IsEnabled: true},
		{Code: "DPM_ACCRUAL_DAILY", Name: "Dự chi lãi tiền gửi (EOD)", Sequence: 15, Endpoint: "http://deposit-service:8080/internal/jobs/deposit-accrual-daily", IsEnabled: true},
		{Code: "LNM_PROVISION_DAILY", Name: "Trích lập dự phòng (EOD)", Sequence: 20, Endpoint: "http://loan-service:8097/internal/jobs/provision-daily", IsEnabled: true},
		// P3a reporting foundation: rebuild fin_trial_balance_daily after
		// the loan steps so statements see the day's accrual/provision posts.
		{Code: "FIN_TRIAL_BALANCE_DAILY", Name: "Tổng hợp số dư hằng ngày (EOD)", Sequence: 30, Endpoint: "http://finance-service:8080/internal/jobs/trial-balance-daily", IsEnabled: true},
	}
	for _, j := range jobs {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO plt_job_definitions (tenant_id, code, name, sequence, endpoint, is_enabled, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,'seed')
			ON CONFLICT (tenant_id, code) DO UPDATE SET endpoint = EXCLUDED.endpoint,
				sequence = EXCLUDED.sequence, updated_at = now()`,
			tenantID, j.Code, j.Name, j.Sequence, j.Endpoint, j.IsEnabled); err != nil {
			return err
		}
	}
	return nil
}

// Run executes the enabled COB sequence for one business date under a
// Postgres advisory lock (single-runner guarantee across replicas — §6
// leader lock). Each step posts to the domain service internal endpoint
// with the tenant + date; the unique (tenant, job, business_date) run row
// makes each step idempotent.
func (s *EODService) Run(ctx context.Context, tenantID, businessDate string) (*RunResult, error) {
	lockConn, err := s.acquireLeaderLock(ctx, tenantID, businessDate)
	if err != nil {
		return nil, err
	}
	defer lockConn.Close()

	rows, err := s.db.QueryContext(ctx, `
		SELECT code, endpoint FROM plt_job_definitions
		WHERE tenant_id = $1 AND is_enabled
		ORDER BY sequence, code`, tenantID)
	if err != nil {
		return nil, err
	}
	type job struct{ code, endpoint string }
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.code, &j.endpoint); err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].code < jobs[j].code })

	result := &RunResult{BusinessDate: businessDate}
	for _, j := range jobs {
		// Idempotency: DONE run for same date is skipped.
		var status string
		err := s.db.QueryRowContext(ctx, `
			SELECT status FROM plt_job_runs WHERE tenant_id = $1 AND job_code = $2 AND business_date = $3::date`,
			tenantID, j.code, businessDate).Scan(&status)
		if err == nil && status == "DONE" {
			result.Steps = append(result.Steps, StepResult{JobCode: j.code, Status: "SKIPPED_DONE"})
			continue
		}

		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO plt_job_runs (tenant_id, job_code, business_date, status)
			VALUES ($1,$2,$3::date,'RUNNING')
			ON CONFLICT (tenant_id, job_code, business_date) DO UPDATE SET status = 'RUNNING',
				started_at = now(), finished_at = NULL, error = NULL`,
			tenantID, j.code, businessDate); err != nil {
			return nil, err
		}

		step := s.runStep(ctx, tenantID, j.code, j.endpoint, businessDate)
		result.Steps = append(result.Steps, step)
	}
	return result, nil
}

// acquireLeaderLock takes a session-level advisory lock on a dedicated
// connection; only one EOD run per tenant+date proceeds cluster-wide. The
// lock is released when the connection closes.
func (s *EODService) acquireLeaderLock(ctx context.Context, tenantID, businessDate string) (*sql.Conn, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock connection: %w", err)
	}
	lockKey := fmt.Sprintf("eod:%s:%s", tenantID, businessDate)
	var acquired bool
	err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, lockKey).Scan(&acquired)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("advisory lock: %w", err)
	}
	if !acquired {
		conn.Close()
		return nil, fmt.Errorf("another EOD run holds the lock for %s %s", tenantID, businessDate)
	}
	s.logger.Info("eod leader lock acquired", "lock", lockKey)
	return conn, nil
}

func (s *EODService) runStep(ctx context.Context, tenantID, code, endpoint, businessDate string) StepResult {
	step := StepResult{JobCode: code}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?to_date="+businessDate, nil)
	if err != nil {
		step.Status = "FAILED"
		step.Error = err.Error()
		s.markRun(ctx, tenantID, code, businessDate, "FAILED", err.Error())
		return step
	}
	req.Header.Set("X-Tenant-Id", tenantID)
	req.Header.Set("X-User-Id", "eod-job")
	resp, err := s.client.Do(req)
	if err != nil {
		step.Status = "FAILED"
		step.Error = err.Error()
		s.markRun(ctx, tenantID, code, businessDate, "FAILED", err.Error())
		return step
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		step.Status = "FAILED"
		step.Error = fmt.Sprintf("upstream %d: %s", resp.StatusCode, truncate(string(body), 300))
		s.markRun(ctx, tenantID, code, businessDate, "FAILED", step.Error)
		return step
	}
	step.Status = "DONE"
	s.markRun(ctx, tenantID, code, businessDate, "DONE", "")
	return step
}

func (s *EODService) markRun(ctx context.Context, tenantID, code, businessDate, status, errMsg string) {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE plt_job_runs SET status = $4, error = $5, finished_at = now()
		WHERE tenant_id = $1 AND job_code = $2 AND business_date = $3::date`,
		tenantID, code, businessDate, status, errMsg); err != nil {
		s.logger.Error("eod mark run", "job", code, "err", err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// JobRun is one COB run history row (W6 jobs UI).
type JobRun struct {
	JobCode      string `json:"job_code"`
	BusinessDate string `json:"business_date"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
}

// ListJobs returns the configured COB steps.
func (s *EODService) ListJobs(ctx context.Context, tenantID string) ([]JobDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, name, sequence, endpoint, is_enabled
		FROM plt_job_definitions WHERE tenant_id = $1 ORDER BY sequence, code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobDefinition{}
	for rows.Next() {
		var j JobDefinition
		if err := rows.Scan(&j.Code, &j.Name, &j.Sequence, &j.Endpoint, &j.IsEnabled); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ListJobRuns returns recent run history (optionally one job code).
func (s *EODService) ListJobRuns(ctx context.Context, tenantID, jobCode string, limit int) ([]JobRun, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if jobCode != "" {
		args = append(args, jobCode)
		where = append(where, fmt.Sprintf("job_code = $%d", len(args)))
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT job_code, business_date::text, status, COALESCE(error,''),
		       COALESCE(started_at::text,''), COALESCE(finished_at::text,'')
		FROM plt_job_runs WHERE %s
		ORDER BY business_date DESC, started_at DESC NULLS LAST
		LIMIT $%d`, strings.Join(where, " AND "), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobRun{}
	for rows.Next() {
		var run JobRun
		if err := rows.Scan(&run.JobCode, &run.BusinessDate, &run.Status, &run.Error,
			&run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
