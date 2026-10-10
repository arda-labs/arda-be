package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

type JobExecutor struct {
	targets map[string]string
	secret  string
	client  *http.Client
	sleep   func(context.Context, time.Duration) error
}

func NewJobExecutor(targets map[string]string, secret string, client *http.Client) *JobExecutor {
	if client == nil {
		client = &http.Client{}
	}
	return &JobExecutor{targets: targets, secret: secret, client: client, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (e *JobExecutor) Configured(ownerService string) bool {
	return strings.TrimSpace(e.targets[ownerService]) != ""
}

type jobResponse struct {
	Status       string          `json:"status"`
	Summary      json.RawMessage `json:"summary"`
	Metrics      json.RawMessage `json:"metrics"`
	ErrorCode    string          `json:"error_code"`
	ErrorMessage string          `json:"error_message"`
}

type attemptResult struct {
	outcome   domain.RunOutcome
	retryable bool
}

func (e *JobExecutor) Execute(ctx context.Context, job domain.ScheduledJob, run domain.ScheduledJobRun) domain.RunOutcome {
	if !e.Configured(job.OwnerService) {
		return domain.RunOutcome{Status: domain.JobStatusFailed, ErrorCode: "JOB_TARGET_NOT_CONFIGURED",
			ErrorMessage: "no target URL configured for " + job.OwnerService}
	}
	maxAttempts := job.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var last attemptResult
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			wait := time.Duration(job.RetryBackoffSeconds) * time.Second * time.Duration(1<<uint(attempt-2))
			if err := e.sleep(ctx, wait); err != nil {
				last.outcome.Attempt = attempt - 1
				return terminal(last.outcome)
			}
		}
		if ctx.Err() != nil {
			last.outcome.Attempt = attempt - 1
			return terminal(last.outcome)
		}
		last = e.attempt(ctx, job, run, attempt)
		last.outcome.Attempt = attempt
		if !last.retryable {
			return terminal(last.outcome)
		}
	}
	return terminal(last.outcome)
}

// terminal guarantees Finalize receives SUCCEEDED, FAILED or TIMED_OUT: an
// outcome without a status (cancelled before any attempt finished) is FAILED.
func terminal(o domain.RunOutcome) domain.RunOutcome {
	switch o.Status {
	case domain.JobStatusSucceeded, domain.JobStatusFailed, domain.JobStatusTimedOut:
		return o
	}
	return domain.RunOutcome{Status: domain.JobStatusFailed, Attempt: o.Attempt,
		ErrorCode: "JOB_CANCELLED", ErrorMessage: "run cancelled before an attempt completed"}
}

func (e *JobExecutor) attempt(ctx context.Context, job domain.ScheduledJob, run domain.ScheduledJobRun, attempt int) attemptResult {
	actx, cancel := context.WithTimeout(ctx, time.Duration(job.TimeoutSeconds)*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"run_id": run.ID, "job_code": job.Code, "trigger": run.Trigger,
		"scheduled_for": run.ScheduledFor.UTC().Format(time.RFC3339), "attempt": attempt, "params": run.Params,
	})
	url := strings.TrimRight(e.targets[job.OwnerService], "/") + job.Path
	req, err := http.NewRequestWithContext(actx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return attemptResult{outcome: failed("JOB_REQUEST_INVALID", err.Error())}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "scheduler")
	if err := identity.SignRequest(req, e.secret, "platform-service", job.OwnerService, time.Now(), 5*time.Minute); err != nil {
		return attemptResult{outcome: failed("JOB_SIGNING_FAILED", err.Error())}
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return transportFailure(ctx, actx, job, "no response", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(domain.MaxResultBytes)+1))
	if err != nil {
		return transportFailure(ctx, actx, job, "response body not completed", err)
	}
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests:
		return attemptResult{outcome: failed("UPSTREAM_"+fmt.Sprint(resp.StatusCode), snippet(raw)), retryable: true}
	case resp.StatusCode >= 400:
		return attemptResult{outcome: failed("UPSTREAM_4XX", fmt.Sprintf("HTTP %d: %s", resp.StatusCode, snippet(raw)))}
	}
	if len(raw) > domain.MaxResultBytes {
		return attemptResult{outcome: failed("INVALID_JOB_RESPONSE", "response exceeds 256 KB")}
	}
	var jr jobResponse
	if err := json.Unmarshal(raw, &jr); err != nil || (jr.Status != "succeeded" && jr.Status != "failed") {
		return attemptResult{outcome: failed("INVALID_JOB_RESPONSE", "response is not a job result: "+snippet(raw))}
	}
	result := encodeResult(jr)
	if jr.Status == "succeeded" {
		return attemptResult{outcome: domain.RunOutcome{Status: domain.JobStatusSucceeded, Result: result}}
	}
	code := domain.Truncate(strings.TrimSpace(jr.ErrorCode), domain.MaxErrorCodeLen)
	if code == "" {
		code = "JOB_REPORTED_FAILURE"
	}
	return attemptResult{outcome: domain.RunOutcome{Status: domain.JobStatusFailed, ErrorCode: code, ErrorMessage: domain.Truncate(jr.ErrorMessage, 2000), Result: result}}
}

// transportFailure classifies an error from sending the request or reading the
// response body. A cancelled parent ctx (shutdown, caller gave up) is final;
// the attempt's own deadline is TIMED_OUT and anything else is UNREACHABLE,
// both retryable.
func transportFailure(ctx, actx context.Context, job domain.ScheduledJob, what string, err error) attemptResult {
	if ctx.Err() != nil {
		return attemptResult{outcome: failed("JOB_CANCELLED", "run cancelled: "+err.Error())}
	}
	if errors.Is(actx.Err(), context.DeadlineExceeded) {
		return attemptResult{outcome: domain.RunOutcome{Status: domain.JobStatusTimedOut, ErrorCode: "JOB_TIMEOUT",
			ErrorMessage: fmt.Sprintf("%s within %ds", what, job.TimeoutSeconds)}, retryable: true}
	}
	return attemptResult{outcome: failed("JOB_UNREACHABLE", err.Error()), retryable: true}
}

// encodeResult stores summary/metrics without HTML escaping (which would grow
// <, > and & sixfold) and without the encoder's trailing newline.
func encodeResult(jr jobResponse) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]json.RawMessage{"summary": orNull(jr.Summary), "metrics": orNull(jr.Metrics)})
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n"))
}

func failed(code, msg string) domain.RunOutcome {
	return domain.RunOutcome{Status: domain.JobStatusFailed, ErrorCode: code, ErrorMessage: domain.Truncate(msg, 2000)}
}

func snippet(b []byte) string { return domain.Truncate(strings.TrimSpace(string(b)), 300) }

func orNull(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}
