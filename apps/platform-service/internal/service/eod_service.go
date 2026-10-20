package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
)

var (
	ErrEODBusinessDateUnavailable = errors.New("EOD business-date source is unavailable")
	ErrEODInvalidBusinessDate     = errors.New("invalid EOD business date")
	ErrEODRunInProgress           = errors.New("another SYSTEM EOD run is in progress")
)

// EODService runs the COB job sequence: ordered, idempotent per business
// date, with checkpoint rows in plt_job_runs (service-boundary-design §6).
type EODService struct {
	db       *sql.DB
	client   *http.Client
	logger   *slog.Logger
	calendar ardaBusinessDate.Calendar
	tenants  EODTenantDirectory
	eodDate  EODDateController
	// jobTargets maps owner service -> base URL (JOB_TARGET_URLS).
	jobTargets map[string]string
}

type EODTenantDirectory interface {
	ListActiveTenants(context.Context) ([]string, error)
}

type EODDateController interface {
	BeginEOD(context.Context) (*domain.SystemDate, error)
	ReleaseEOD(context.Context) error
	CompleteEOD(context.Context, string) (*domain.SystemDate, error)
	GetSystemDate(context.Context, string) (*domain.SystemDate, error)
}

// NewEODService wires the COB runner. jobTargets is the JOB_TARGET_URLS map
// (owner service -> base URL); SeedJobs reads it at seed time.
func NewEODService(db *sql.DB, logger *slog.Logger, calendar ardaBusinessDate.Calendar, tenants EODTenantDirectory, jobTargets map[string]string) *EODService {
	service := &EODService{db: db, client: &http.Client{Timeout: 5 * time.Minute}, logger: logger, jobTargets: jobTargets}
	service.calendar = calendar
	service.tenants = tenants
	if lifecycle, ok := calendar.(EODDateController); ok {
		service.eodDate = lifecycle
	}
	return service
}

func resolveEODBusinessDate(ctx context.Context, calendar ardaBusinessDate.Calendar, tenantID, requested string) (string, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", fmt.Errorf("EOD tenant scope is required")
	}
	if requested != "" {
		parsed, err := time.Parse("2006-01-02", requested)
		if err != nil || parsed.Format("2006-01-02") != requested {
			return "", fmt.Errorf("EOD business date must use YYYY-MM-DD")
		}
		return requested, nil
	}
	date, err := ardaBusinessDate.BusinessDate(ctx, calendar, ardaBusinessDate.Scope{TenantID: tenantID, Type: ardaBusinessDate.ScopeSystem})
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrEODBusinessDateUnavailable, err)
	}
	return date.Format("2006-01-02"), nil
}

// JobDefinition is one EOD step.
type JobDefinition struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Sequence  int    `json:"sequence"`
	Endpoint  string `json:"endpoint"`
	IsEnabled bool   `json:"is_enabled"`
}

// EODStepDefinition configures one idempotent internal step and its execution
// contract within an end-of-day run.
type EODStepDefinition struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	Module     string   `json:"module"`
	Order      int      `json:"order"`
	DependsOn  []string `json:"depends_on"`
	Mandatory  bool     `json:"mandatory"`
	StopOnFail bool     `json:"stop_on_fail"`
	Retryable  bool     `json:"retryable"`
	Endpoint   string   `json:"endpoint"`
}

func orderEODSteps(steps []EODStepDefinition) ([]EODStepDefinition, error) {
	byCode := make(map[string]EODStepDefinition, len(steps))
	indegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))
	for _, step := range steps {
		code := strings.TrimSpace(step.Code)
		if code == "" {
			return nil, fmt.Errorf("EOD step code is required")
		}
		if _, exists := byCode[code]; exists {
			return nil, fmt.Errorf("duplicate EOD step code %q", code)
		}
		step.Code = code
		byCode[code] = step
		indegree[code] = 0
	}
	for _, step := range byCode {
		seen := make(map[string]struct{}, len(step.DependsOn))
		for _, dependency := range step.DependsOn {
			dependency = strings.TrimSpace(dependency)
			if _, exists := byCode[dependency]; !exists {
				return nil, fmt.Errorf("EOD step %q depends on unknown step %q", step.Code, dependency)
			}
			if _, exists := seen[dependency]; exists {
				return nil, fmt.Errorf("EOD step %q declares dependency %q more than once", step.Code, dependency)
			}
			seen[dependency] = struct{}{}
			indegree[step.Code]++
			dependents[dependency] = append(dependents[dependency], step.Code)
		}
	}

	ordered := make([]EODStepDefinition, 0, len(steps))
	for len(ordered) < len(steps) {
		ready := make([]EODStepDefinition, 0)
		for code, degree := range indegree {
			if degree == 0 {
				ready = append(ready, byCode[code])
			}
		}
		if len(ready) == 0 {
			return nil, fmt.Errorf("EOD step dependencies contain a cycle")
		}
		sort.Slice(ready, func(i, j int) bool {
			if ready[i].Order != ready[j].Order {
				return ready[i].Order < ready[j].Order
			}
			return ready[i].Code < ready[j].Code
		})
		next := ready[0]
		ordered = append(ordered, next)
		delete(indegree, next.Code)
		for _, dependent := range dependents[next.Code] {
			indegree[dependent]--
		}
	}
	return ordered, nil
}

func eodStepBlockReason(step EODStepDefinition, states map[string]string, stopTenant bool) string {
	if stopTenant {
		return "stopped after a previous step failure"
	}
	for _, dependency := range step.DependsOn {
		if states[dependency] != "DONE" {
			return "dependency did not succeed: " + dependency
		}
	}
	return ""
}

// RunResult summarizes one COB execution.
type RunResult struct {
	BusinessDate      string             `json:"business_date"`
	BusinessDateState *domain.SystemDate `json:"business_date_state,omitempty"`
	Steps             []StepResult       `json:"steps"`
}

// StepResult is one job step outcome.
type StepResult struct {
	TenantID string `json:"tenant_id"`
	JobCode  string `json:"job_code"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	// Processed/Failed là trạng thái có cấu trúc mà bước tự báo trong
	// result.processed/result.failed (xem eodStepOutcome).
	Processed int `json:"processed,omitempty"`
	Failed    int `json:"failed,omitempty"`
}

// eodStepTemplate is one COB step of the fixed catalog. The owner service
// base URL is not part of the template: it comes from JOB_TARGET_URLS at seed
// time (see EODModuleServices), and Path is appended to it.
type eodStepTemplate struct {
	Code       string
	Name       string
	Module     string
	Order      int
	DependsOn  []string
	Path       string
	Mandatory  bool
	StopOnFail bool
	Retryable  bool
}

// eodStepCatalog is the default COB sequence (loan accrual -> provision ->
// snapshot -> trial balance -> reporting).
var eodStepCatalog = []eodStepTemplate{
	{Code: "LNM_ACCRUAL_DAILY", Name: "Tính lãi cho vay (EOD)", Module: "loan", Order: 10, DependsOn: []string{}, Path: "/internal/jobs/accrual-daily", Mandatory: true, StopOnFail: true, Retryable: true},
	{Code: "DPM_ACCRUAL_DAILY", Name: "Dự chi lãi tiền gửi (EOD)", Module: "deposit", Order: 15, DependsOn: []string{}, Path: "/internal/jobs/deposit-accrual-daily", Mandatory: true, StopOnFail: true, Retryable: true},
	{Code: "LNM_PROVISION_DAILY", Name: "Trích lập dự phòng (EOD)", Module: "loan", Order: 20, DependsOn: []string{"LNM_ACCRUAL_DAILY"}, Path: "/internal/jobs/provision-daily", Mandatory: true, StopOnFail: true, Retryable: true},
	{Code: "LNM_AGREEMENT_DAILY_SNAPSHOT", Name: "Chụp số dư khế ước (EOD)", Module: "loan", Order: 25, DependsOn: []string{"LNM_PROVISION_DAILY"}, Path: "/internal/jobs/agreement-daily-snapshot", Mandatory: true, StopOnFail: true, Retryable: true},
	// P3a reporting foundation: rebuild fin_trial_balance_daily after
	// the loan steps so statements see the day's accrual/provision posts.
	{Code: "FIN_TRIAL_BALANCE_DAILY", Name: "Tổng hợp số dư hằng ngày (EOD)", Module: "finance", Order: 30, DependsOn: []string{"LNM_ACCRUAL_DAILY", "DPM_ACCRUAL_DAILY", "LNM_PROVISION_DAILY", "LNM_AGREEMENT_DAILY_SNAPSHOT"}, Path: "/internal/jobs/trial-balance-daily", Mandatory: true, StopOnFail: true, Retryable: true},
	// Reporting data layer: materialise the fact read model from the
	// domain services after the day's posts so period reports see the
	// as-of snapshot (arda-be/docs/reporting-data-layer.md).
	{Code: "RPT_EXTRACT_DAILY", Name: "Trích xuất dữ liệu báo cáo (EOD)", Module: "statistical", Order: 35, DependsOn: []string{"FIN_TRIAL_BALANCE_DAILY"}, Path: "/internal/jobs/report-extract-daily", Mandatory: true, StopOnFail: true, Retryable: true},
	// Verify the accounting indicators against the trial balance the ETL
	// just materialised: a wrong account mapping surfaces at COB rather
	// than on a published balance sheet.
	{Code: "RPT_RECONCILE_ACCOUNTING", Name: "Đối soát chỉ tiêu tài chính kế toán (EOD)", Module: "statistical", Order: 38, DependsOn: []string{"RPT_EXTRACT_DAILY"}, Path: "/internal/jobs/reconcile-accounting", Mandatory: true, StopOnFail: true, Retryable: true},
	// Proactive reporting: judge the period's computed results against the
	// threshold rules and raise alerts. Last, so the numbers are final.
	{Code: "RPT_EVALUATE_RULES", Name: "Đánh giá ngưỡng cảnh báo chỉ tiêu (EOD)", Module: "statistical", Order: 40, DependsOn: []string{"RPT_RECONCILE_ACCOUNTING"}, Path: "/internal/jobs/evaluate-rules", Mandatory: true, StopOnFail: true, Retryable: true},
}

// EODModuleServices maps an EOD step module to the owner service whose base
// URL must be present in JOB_TARGET_URLS (the same map the scheduled-job
// executor uses, keyed by owner_service).
var EODModuleServices = map[string]string{
	"loan":        "loan-service",
	"deposit":     "deposit-service",
	"finance":     "finance-service",
	"statistical": "statistical-service",
}

// ErrEODJobTargetMissing is returned by SeedJobs when JOB_TARGET_URLS lacks
// the owner service of a catalog step. Seeding fails closed: no step is
// written with an empty or guessed endpoint.
var ErrEODJobTargetMissing = errors.New("EOD job target is not configured")

// resolveEODSteps turns the catalog into definitions whose Endpoint is the
// configured owner-service base URL plus the step path. Every missing target
// is reported in one error.
func (s *EODService) resolveEODSteps() ([]EODStepDefinition, error) {
	var missing []string
	steps := make([]EODStepDefinition, 0, len(eodStepCatalog))
	for _, step := range eodStepCatalog {
		owner, ok := EODModuleServices[step.Module]
		if !ok {
			return nil, fmt.Errorf("EOD step %s: module %q has no owner service", step.Code, step.Module)
		}
		base := strings.TrimSpace(s.jobTargets[owner])
		if base == "" {
			missing = append(missing, step.Code+" ("+owner+")")
			continue
		}
		steps = append(steps, EODStepDefinition{
			Code: step.Code, Name: step.Name, Module: step.Module, Order: step.Order, DependsOn: step.DependsOn,
			Mandatory: step.Mandatory, StopOnFail: step.StopOnFail, Retryable: step.Retryable,
			Endpoint: strings.TrimRight(base, "/") + step.Path,
		})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: set JOB_TARGET_URLS for %s", ErrEODJobTargetMissing, strings.Join(missing, ", "))
	}
	return steps, nil
}

// SeedJobs upserts the default COB sequence for a tenant, with endpoints
// resolved from JOB_TARGET_URLS at call time. It fails before writing
// anything when a target is missing.
func (s *EODService) SeedJobs(ctx context.Context, tenantID string) error {
	jobs, err := s.resolveEODSteps()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		dependsOn := j.DependsOn
		if dependsOn == nil {
			dependsOn = []string{}
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO plt_job_definitions (tenant_id, code, name, sequence, endpoint, is_enabled, created_by, module, depends_on, mandatory, stop_on_fail, retryable)
			VALUES ($1,$2,$3,$4,$5,true,'seed',$6,$7,$8,$9,$10)
			ON CONFLICT (tenant_id, code) DO UPDATE SET endpoint = EXCLUDED.endpoint,
				sequence = EXCLUDED.sequence, module=EXCLUDED.module, depends_on=EXCLUDED.depends_on,
				mandatory=EXCLUDED.mandatory, stop_on_fail=EXCLUDED.stop_on_fail,
				retryable=EXCLUDED.retryable, updated_at = now()
			WHERE plt_job_definitions.created_by = 'seed'`,
			tenantID, j.Code, j.Name, j.Order, j.Endpoint, j.Module, dependsOn, j.Mandatory, j.StopOnFail, j.Retryable); err != nil {
			return err
		}
	}
	return nil
}

func (s *EODService) RunSystem(ctx context.Context, requestedDate string) (*RunResult, error) {
	if s.calendar == nil || s.eodDate == nil || s.tenants == nil {
		return nil, ErrEODBusinessDateUnavailable
	}
	businessDate := strings.TrimSpace(requestedDate)
	if businessDate != "" {
		parsed, parseErr := time.Parse("2006-01-02", businessDate)
		if parseErr != nil || parsed.Format("2006-01-02") != businessDate {
			return nil, fmt.Errorf("%w: date must use YYYY-MM-DD", ErrEODInvalidBusinessDate)
		}
		var previousStatus string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM plt_eod_runs WHERE eod_date=$1::date`, businessDate).Scan(&previousStatus)
		if err == nil && previousStatus == "SUCCEEDED" {
			state, stateErr := s.eodDate.GetSystemDate(ctx, "HEAD_OFFICE")
			if stateErr != nil {
				return nil, fmt.Errorf("%w: %w", ErrEODBusinessDateUnavailable, stateErr)
			}
			return &RunResult{BusinessDate: businessDate, BusinessDateState: state, Steps: []StepResult{}}, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	current, err := ardaBusinessDate.BusinessDate(ctx, s.calendar, ardaBusinessDate.Scope{Type: ardaBusinessDate.ScopeSystem})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEODBusinessDateUnavailable, err)
	}
	if businessDate == "" {
		businessDate = current.Format("2006-01-02")
	} else if businessDate != current.Format("2006-01-02") {
		return nil, fmt.Errorf("%w: must match current SYSTEM business date %s", ErrEODInvalidBusinessDate, current.Format("2006-01-02"))
	}

	lockConn, err := s.acquireLeaderLock(ctx, "SYSTEM", businessDate)
	if err != nil {
		return nil, err
	}
	defer lockConn.Close()
	var previousStatus string
	err = s.db.QueryRowContext(ctx, `SELECT status FROM plt_eod_runs WHERE eod_date=$1::date`, businessDate).Scan(&previousStatus)
	if err == nil && previousStatus == "SUCCEEDED" {
		return &RunResult{BusinessDate: businessDate, Steps: []StepResult{}}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	lockedDate, err := s.eodDate.BeginEOD(ctx)
	if err != nil {
		return nil, err
	}
	dateLocked := true
	defer func() {
		if !dateLocked {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if releaseErr := s.eodDate.ReleaseEOD(releaseCtx); releaseErr != nil {
			s.logger.Error("failed to release SYSTEM EOD lock", "err", releaseErr)
		}
	}()
	if lockedDate.CurrentBusinessDate.Format("2006-01-02") != businessDate {
		return nil, fmt.Errorf("SYSTEM business date changed before EOD lock was acquired")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO plt_eod_runs (eod_date, scope_type, status)
		VALUES ($1::date,'SYSTEM','RUNNING') ON CONFLICT (eod_date) DO UPDATE
		SET status='RUNNING', started_at=now(), finished_at=NULL, error=NULL WHERE plt_eod_runs.status <> 'SUCCEEDED'`, businessDate); err != nil {
		return nil, err
	}

	tenantIDs, err := s.tenants.ListActiveTenants(ctx)
	if err != nil {
		return nil, s.failEOD(ctx, businessDate, fmt.Errorf("discover active tenants: %w", err))
	}
	if len(tenantIDs) == 0 {
		return nil, s.failEOD(ctx, businessDate, errors.New("SYSTEM EOD cannot advance with no active tenants"))
	}
	result := &RunResult{BusinessDate: businessDate, Steps: []StepResult{}}
	mandatoryFailure := false
	for _, tenantID := range tenantIDs {
		if strings.TrimSpace(tenantID) == "" {
			return nil, s.failEOD(ctx, businessDate, errors.New("IAM returned an empty active tenant ID"))
		}
		if err := s.SeedJobs(ctx, tenantID); err != nil {
			return nil, s.failEOD(ctx, businessDate, fmt.Errorf("seed EOD steps for tenant %s: %w", tenantID, err))
		}
		steps, err := s.listEODSteps(ctx, tenantID)
		if err != nil {
			return nil, s.failEOD(ctx, businessDate, fmt.Errorf("load EOD steps for tenant %s: %w", tenantID, err))
		}
		if len(steps) == 0 {
			return nil, s.failEOD(ctx, businessDate, fmt.Errorf("tenant %s has no enabled EOD steps", tenantID))
		}
		steps, err = orderEODSteps(steps)
		if err != nil {
			return nil, s.failEOD(ctx, businessDate, fmt.Errorf("invalid EOD graph for tenant %s: %w", tenantID, err))
		}
		states := make(map[string]string, len(steps))
		stopTenant := false
		for _, definition := range steps {
			blockReason := eodStepBlockReason(definition, states, stopTenant)
			if blockReason != "" {
				step := StepResult{TenantID: tenantID, JobCode: definition.Code, Status: "SKIPPED_DEPENDENCY"}
				step.Error = blockReason
				if err := s.startJobRun(ctx, tenantID, definition.Code, businessDate); err != nil {
					return nil, s.failEOD(ctx, businessDate, err)
				}
				if err := s.markRun(ctx, tenantID, definition.Code, businessDate, "SKIPPED", step.Error); err != nil {
					return nil, s.failEOD(ctx, businessDate, err)
				}
				result.Steps = append(result.Steps, step)
				states[definition.Code] = step.Status
				mandatoryFailure = mandatoryFailure || definition.Mandatory
				continue
			}
			prior, getErr := s.jobRunStatus(ctx, tenantID, definition.Code, businessDate)
			if getErr != nil {
				return nil, s.failEOD(ctx, businessDate, getErr)
			}
			if prior == "DONE" {
				result.Steps = append(result.Steps, StepResult{TenantID: tenantID, JobCode: definition.Code, Status: "SKIPPED_DONE"})
				states[definition.Code] = "DONE"
				continue
			}
			if prior == "FAILED" && !definition.Retryable {
				step := StepResult{TenantID: tenantID, JobCode: definition.Code, Status: "FAILED", Error: "step is not retryable"}
				result.Steps = append(result.Steps, step)
				states[definition.Code] = "FAILED"
				mandatoryFailure = mandatoryFailure || definition.Mandatory
				stopTenant = definition.StopOnFail
				continue
			}
			if err := s.startJobRun(ctx, tenantID, definition.Code, businessDate); err != nil {
				return nil, s.failEOD(ctx, businessDate, err)
			}
			step := s.runStep(ctx, tenantID, definition.Code, definition.Endpoint, businessDate)
			result.Steps = append(result.Steps, step)
			states[definition.Code] = step.Status
			if step.Status != "DONE" {
				mandatoryFailure = mandatoryFailure || definition.Mandatory
				stopTenant = definition.StopOnFail
			}
		}
	}
	if mandatoryFailure {
		return result, s.failEOD(ctx, businessDate, errors.New("one or more mandatory EOD steps failed"))
	}
	newDate, err := s.eodDate.CompleteEOD(ctx, businessDate)
	if err != nil {
		return result, s.failEOD(ctx, businessDate, fmt.Errorf("complete SYSTEM business date: %w", err))
	}
	dateLocked = false
	result.BusinessDateState = newDate
	return result, nil
}

func (s *EODService) listEODSteps(ctx context.Context, tenantID string) ([]EODStepDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code, name, module, sequence, to_json(depends_on)::text, mandatory, stop_on_fail, retryable, endpoint
		FROM plt_job_definitions WHERE tenant_id=$1 AND is_enabled ORDER BY sequence,code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	steps := make([]EODStepDefinition, 0)
	for rows.Next() {
		var item EODStepDefinition
		var dependencies string
		if err := rows.Scan(&item.Code, &item.Name, &item.Module, &item.Order, &dependencies, &item.Mandatory, &item.StopOnFail, &item.Retryable, &item.Endpoint); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(dependencies), &item.DependsOn); err != nil {
			return nil, fmt.Errorf("decode EOD step %s dependencies: %w", item.Code, err)
		}
		steps = append(steps, item)
	}
	return steps, rows.Err()
}

func (s *EODService) jobRunStatus(ctx context.Context, tenantID, code, businessDate string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM plt_job_runs WHERE tenant_id=$1 AND job_code=$2 AND business_date=$3::date`, tenantID, code, businessDate).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return status, err
}

func (s *EODService) startJobRun(ctx context.Context, tenantID, code, businessDate string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO plt_job_runs (tenant_id,job_code,business_date,status,attempt)
		VALUES ($1,$2,$3::date,'RUNNING',1) ON CONFLICT (tenant_id,job_code,business_date) DO UPDATE
		SET status='RUNNING',attempt=plt_job_runs.attempt+1,started_at=now(),finished_at=NULL,error=NULL`, tenantID, code, businessDate)
	return err
}

func (s *EODService) failEOD(ctx context.Context, businessDate string, cause error) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE plt_eod_runs SET status='FAILED',finished_at=now(),error=$2 WHERE eod_date=$1::date AND status='RUNNING'`, businessDate, truncate(cause.Error(), 1000)); err != nil {
		s.logger.Error("mark EOD failed", "date", businessDate, "err", err)
	}
	return cause
}

// acquireLeaderLock takes a session-level advisory lock on a dedicated
// connection; only one EOD run per SYSTEM date proceeds cluster-wide. The
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
		return nil, fmt.Errorf("%w (%s %s)", ErrEODRunInProgress, tenantID, businessDate)
	}
	s.logger.Info("eod leader lock acquired", "lock", lockKey)
	return conn, nil
}

func (s *EODService) runStep(ctx context.Context, tenantID, code, endpoint, businessDate string) StepResult {
	step := StepResult{TenantID: tenantID, JobCode: code}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?to_date="+businessDate, nil)
	if err != nil {
		step.Status = "FAILED"
		step.Error = err.Error()
		if markErr := s.markRun(ctx, tenantID, code, businessDate, "FAILED", err.Error()); markErr != nil {
			step.Error += "; persist failure: " + markErr.Error()
		}
		return step
	}
	req.Header.Set("X-Tenant-Id", tenantID)
	req.Header.Set("X-User-Id", "eod-job")
	resp, err := s.client.Do(req)
	if err != nil {
		step.Status = "FAILED"
		step.Error = err.Error()
		if markErr := s.markRun(ctx, tenantID, code, businessDate, "FAILED", err.Error()); markErr != nil {
			step.Error += "; persist failure: " + markErr.Error()
		}
		return step
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		step.Status = "FAILED"
		step.Error = fmt.Sprintf("upstream %d: %s", resp.StatusCode, truncate(string(body), 300))
		if markErr := s.markRun(ctx, tenantID, code, businessDate, "FAILED", step.Error); markErr != nil {
			step.Error += "; persist failure: " + markErr.Error()
		}
		return step
	}
	// HTTP 2xx chưa đủ: bước xử lý theo từng khế ước/sổ có thể trả 200 kèm
	// danh sách lỗi một phần. Bất kỳ failed > 0 (hoặc thân không đọc được) là
	// FAILED để ngày không tiến và bước phụ thuộc không chạy trên dữ liệu thiếu.
	outcome, err := eodStepOutcome(body)
	step.Processed = outcome.Processed
	step.Failed = outcome.Failed
	if err != nil || outcome.Failed > 0 {
		step.Status = "FAILED"
		if err != nil {
			step.Error = "unreadable step status: " + err.Error()
		} else {
			step.Error = fmt.Sprintf("step reported %d failed item(s): %s", outcome.Failed, truncate(outcome.Detail, 300))
		}
		if markErr := s.markRun(ctx, tenantID, code, businessDate, "FAILED", step.Error); markErr != nil {
			step.Error += "; persist failure: " + markErr.Error()
		}
		return step
	}
	step.Status = "DONE"
	if err := s.markRun(ctx, tenantID, code, businessDate, "DONE", ""); err != nil {
		step.Status = "FAILED"
		step.Error = "persist successful step checkpoint: " + err.Error()
	}
	return step
}

// eodStepStatus là trạng thái có cấu trúc một bước EOD trả về trong phong bì
// thành công: result.processed (số mục đã xử lý) và result.failed (số nguyên
// hoặc mảng mô tả mục lỗi, ví dụ RunResult.FailedDetail của tính lãi cho vay).
type eodStepStatus struct {
	Processed int
	Failed    int
	Detail    string
}

// eodStepOutcome đọc thân phản hồi 2xx của một bước EOD. Thân rỗng (204) nghĩa
// là bước không báo chi tiết. success=false hoặc failed > 0 tính là lỗi; thân
// không phải JSON hợp lệ trả lỗi để runStep đánh FAILED (fail-closed).
func eodStepOutcome(body []byte) (eodStepStatus, error) {
	var out eodStepStatus
	if len(strings.TrimSpace(string(body))) == 0 {
		return out, nil
	}
	var envelope struct {
		Success *bool           `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return out, fmt.Errorf("decode step response: %w", err)
	}
	if envelope.Success != nil && !*envelope.Success {
		out.Failed = 1
		out.Detail = "step response has success=false"
		return out, nil
	}
	var result map[string]json.RawMessage
	if len(envelope.Result) == 0 || json.Unmarshal(envelope.Result, &result) != nil {
		// Kết quả không phải object (null, mảng, số): không có trạng thái mục.
		return out, nil
	}
	if raw, ok := result["processed"]; ok {
		if err := json.Unmarshal(raw, &out.Processed); err != nil {
			return out, fmt.Errorf("decode result.processed: %w", err)
		}
	}
	raw, ok := result["failed"]
	if !ok || string(raw) == "null" {
		return out, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		out.Failed = len(items)
		details := make([]string, 0, len(items))
		for _, item := range items {
			var text string
			if json.Unmarshal(item, &text) != nil {
				text = string(item)
			}
			details = append(details, text)
		}
		out.Detail = strings.Join(details, "; ")
		return out, nil
	}
	var count int
	if err := json.Unmarshal(raw, &count); err != nil {
		return out, fmt.Errorf("result.failed must be a count or a list: %w", err)
	}
	if count < 0 {
		return out, fmt.Errorf("result.failed must not be negative (%d)", count)
	}
	out.Failed = count
	return out, nil
}

func (s *EODService) markRun(ctx context.Context, tenantID, code, businessDate, status, errMsg string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE plt_job_runs SET status = $4, error = $5, finished_at = now()
		WHERE tenant_id = $1 AND job_code = $2 AND business_date = $3::date`,
		tenantID, code, businessDate, status, errMsg); err != nil {
		return fmt.Errorf("mark EOD step %s run: %w", code, err)
	}
	return nil
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
