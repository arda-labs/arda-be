package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ardaevents "github.com/arda-labs/arda/libs/go/arda-events"
)

const (
	DefaultTimezone = "Asia/Ho_Chi_Minh"

	JobStatusRunning   = "RUNNING"
	JobStatusSucceeded = "SUCCEEDED"
	JobStatusFailed    = "FAILED"
	JobStatusTimedOut  = "TIMED_OUT"
	JobStatusSkipped   = "SKIPPED"

	JobTriggerSchedule = "SCHEDULE"
	JobTriggerManual   = "MANUAL"

	SubjectJobFailed    = ardaevents.SubjectPlatformJobFailed
	SubjectJobRecovered = ardaevents.SubjectPlatformJobRecovered
	EventJobFailed      = ardaevents.EventPlatformJobFailed
	EventJobRecovered   = ardaevents.EventPlatformJobRecovered

	MaxErrorCodeLen    = 64 // plt_scheduled_job_runs.error_code VARCHAR(64)
	MaxErrorMessageLen = 2000
	MaxResultBytes     = 256 << 10
)

// JobDefinition is the code-owned part of a scheduled job. Only these jobs can
// exist; the UI edits operational fields and can never point a job at an URL.
type JobDefinition struct {
	Code                  string
	Name                  string
	OwnerService          string
	Path                  string
	DefaultCron           string // "" = manual only
	DefaultTimeoutSeconds int
	DefaultMaxAttempts    int
	DefaultBackoffSeconds int
	AllowedParams         []string
}

var JobCatalog = []JobDefinition{
	{Code: "ai-eval", Name: "AI retrieval eval", OwnerService: "ai-service", Path: "/internal/jobs/ai-eval",
		DefaultCron: "0 6 * * *", DefaultTimeoutSeconds: 600, DefaultMaxAttempts: 2, DefaultBackoffSeconds: 120},
	{Code: "eod", Name: "End of day (COB)", OwnerService: "platform-service", Path: "/internal/jobs/eod",
		DefaultCron: "", DefaultTimeoutSeconds: 330, DefaultMaxAttempts: 1, DefaultBackoffSeconds: 0,
		AllowedParams: []string{"business_date"}},
	{Code: "platform-runs-cleanup", Name: "Scheduled job history cleanup", OwnerService: "platform-service", Path: "/internal/jobs/platform-runs-cleanup",
		DefaultCron: "30 2 * * *", DefaultTimeoutSeconds: 120, DefaultMaxAttempts: 2, DefaultBackoffSeconds: 60},
}

func FindJobDefinition(code string) (JobDefinition, bool) {
	for _, d := range JobCatalog {
		if d.Code == code {
			return d, true
		}
	}
	return JobDefinition{}, false
}

type ScheduledJob struct {
	Code                string           `json:"code"`
	Name                string           `json:"name"`
	OwnerService        string           `json:"owner_service"`
	Path                string           `json:"path"`
	CronExpr            *string          `json:"cron_expr"`
	Timezone            string           `json:"timezone"`
	Enabled             bool             `json:"enabled"`
	TimeoutSeconds      int              `json:"timeout_seconds"`
	MaxAttempts         int              `json:"max_attempts"`
	RetryBackoffSeconds int              `json:"retry_backoff_seconds"`
	NotifyTenantID      string           `json:"notify_tenant_id"`
	NotifyUserIDs       []string         `json:"notify_user_ids"`
	NotifyGroupIDs      []string         `json:"notify_group_ids"`
	NotifyRoleCodes     []string         `json:"notify_role_codes"`
	NextRunAt           *time.Time       `json:"next_run_at"`
	LastStatus          *string          `json:"last_status"`
	TargetConfigured    bool             `json:"target_configured"`
	RetiredAt           *time.Time       `json:"retired_at"`
	Version             int64            `json:"version"`
	UpdatedAt           time.Time        `json:"updated_at"`
	UpdatedBy           *string          `json:"updated_by"`
	LastRun             *ScheduledJobRun `json:"last_run,omitempty"`
}

type ScheduledJobRun struct {
	ID           string            `json:"id"`
	JobCode      string            `json:"job_code"`
	Trigger      string            `json:"trigger"`
	ScheduledFor time.Time         `json:"scheduled_for"`
	Status       string            `json:"status"`
	Attempt      int               `json:"attempt"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   *time.Time        `json:"finished_at"`
	DeadlineAt   time.Time         `json:"deadline_at"`
	ErrorCode    *string           `json:"error_code"`
	ErrorMessage *string           `json:"error_message"`
	Result       json.RawMessage   `json:"result,omitempty"`
	TriggeredBy  string            `json:"triggered_by"`
	Params       map[string]string `json:"params,omitempty"`
}

// JobConfigUpdate is the only shape the API may write. CronExpr nil = unchanged
// is NOT supported: the client always sends the full operational config.
type JobConfigUpdate struct {
	CronExpr            *string  `json:"cron_expr"` // nil or "" = manual only
	Timezone            string   `json:"timezone"`
	Enabled             bool     `json:"enabled"`
	TimeoutSeconds      int      `json:"timeout_seconds"`
	MaxAttempts         int      `json:"max_attempts"`
	RetryBackoffSeconds int      `json:"retry_backoff_seconds"`
	NotifyUserIDs       []string `json:"notify_user_ids"`
	NotifyGroupIDs      []string `json:"notify_group_ids"`
	NotifyRoleCodes     []string `json:"notify_role_codes"`
	Version             int64    `json:"version"`
}

func (u JobConfigUpdate) Cron() string {
	if u.CronExpr == nil {
		return ""
	}
	return strings.TrimSpace(*u.CronExpr)
}

func (u JobConfigUpdate) Validate() error {
	if u.TimeoutSeconds < 10 || u.TimeoutSeconds > 3600 {
		return fmt.Errorf("%w: timeout_seconds must be 10..3600", ErrFieldOutOfRange)
	}
	if u.MaxAttempts < 1 || u.MaxAttempts > 5 {
		return fmt.Errorf("%w: max_attempts must be 1..5", ErrFieldOutOfRange)
	}
	if u.RetryBackoffSeconds < 0 || u.RetryBackoffSeconds > 3600 {
		return fmt.Errorf("%w: retry_backoff_seconds must be 0..3600", ErrFieldOutOfRange)
	}
	if u.Version < 1 {
		return fmt.Errorf("%w: version is required", ErrFieldOutOfRange)
	}
	if len(u.NotifyUserIDs)+len(u.NotifyGroupIDs)+len(u.NotifyRoleCodes) > 50 {
		return fmt.Errorf("%w: at most 50 recipient selectors", ErrFieldOutOfRange)
	}
	if u.Cron() == "" {
		_, err := loadLocation(u.Timezone)
		return err
	}
	return ValidateSchedule(u.Cron(), u.Timezone)
}

// RunOutcome is what the executor reports for one run.
type RunOutcome struct {
	Status       string
	Attempt      int
	ErrorCode    string
	ErrorMessage string
	Result       json.RawMessage
}

// JobEvent is the payload of arda.platform.job.{failed,recovered}.v1.
type JobEvent struct {
	EventID      string   `json:"event_id"`
	OccurredAt   string   `json:"occurred_at"`
	JobCode      string   `json:"job_code"`
	JobName      string   `json:"job_name"`
	RunID        string   `json:"run_id"`
	Status       string   `json:"status"`
	Attempt      int      `json:"attempt"`
	ErrorCode    string   `json:"error_code"`
	ErrorMessage string   `json:"error_message"`
	TenantID     string   `json:"tenant_id"`
	UserIDs      []string `json:"user_ids"`
	GroupIDs     []string `json:"group_ids"`
	RoleCodes    []string `json:"role_codes"`
	Href         string   `json:"href"`
	Locale       string   `json:"locale"`
}

func isFailure(status string) bool { return status == JobStatusFailed || status == JobStatusTimedOut }

// Transition reports whether moving last_status from prev to next must emit an
// event. SKIPPED never changes last_status and never emits.
func Transition(prev, next string) (subject, eventCode string, ok bool) {
	switch {
	case isFailure(next) && !isFailure(prev):
		return SubjectJobFailed, EventJobFailed, true
	case next == JobStatusSucceeded && isFailure(prev):
		return SubjectJobRecovered, EventJobRecovered, true
	}
	return "", "", false
}

func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
