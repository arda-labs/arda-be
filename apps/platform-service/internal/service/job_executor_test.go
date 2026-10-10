package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

const execSecret = "01234567890123456789012345678901"

func execFixture(t *testing.T, h http.HandlerFunc) (*JobExecutor, domain.ScheduledJob, *int32) {
	t.Helper()
	var calls int32
	protected := identity.RequireServiceAuth(execSecret, "ai-service", identity.AllowedSources("platform-service"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			h(w, r)
		}))
	srv := httptest.NewServer(protected)
	t.Cleanup(srv.Close)
	ex := NewJobExecutor(map[string]string{"ai-service": srv.URL}, execSecret, srv.Client())
	ex.sleep = func(context.Context, time.Duration) error { return nil }
	job := domain.ScheduledJob{Code: "ai-eval", OwnerService: "ai-service", Path: "/internal/jobs/ai-eval",
		TimeoutSeconds: 10, MaxAttempts: 3, RetryBackoffSeconds: 1}
	return ex, job, &calls
}

var execRun = domain.ScheduledJobRun{ID: "r1", JobCode: "ai-eval", Trigger: domain.JobTriggerManual}

func TestExecuteSucceeded(t *testing.T) {
	ex, job, calls := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"succeeded","summary":{"passed":12}}`))
	})
	out := ex.Execute(context.Background(), job, execRun)
	if out.Status != domain.JobStatusSucceeded || out.Attempt != 1 || *calls != 1 || !strings.Contains(string(out.Result), `"passed":12`) {
		t.Fatalf("unexpected outcome %+v calls=%d", out, *calls)
	}
}

func TestExecuteBusinessFailureIsNotRetried(t *testing.T) {
	ex, job, calls := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed","error_code":"EVAL_FAILED","error_message":"12/12 case failed"}`))
	})
	out := ex.Execute(context.Background(), job, execRun)
	if out.Status != domain.JobStatusFailed || out.ErrorCode != "EVAL_FAILED" || *calls != 1 {
		t.Fatalf("unexpected outcome %+v calls=%d", out, *calls)
	}
}

func TestExecuteRetries5xxThenSucceeds(t *testing.T) {
	var n int32
	ex, job, calls := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"status":"succeeded"}`))
	})
	out := ex.Execute(context.Background(), job, execRun)
	if out.Status != domain.JobStatusSucceeded || out.Attempt != 3 || *calls != 3 {
		t.Fatalf("unexpected outcome %+v calls=%d", out, *calls)
	}
}

func TestExecute4xxIsTerminal(t *testing.T) {
	ex, job, calls := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})
	out := ex.Execute(context.Background(), job, execRun)
	if out.Status != domain.JobStatusFailed || out.ErrorCode != "UPSTREAM_4XX" || *calls != 1 {
		t.Fatalf("unexpected outcome %+v calls=%d", out, *calls)
	}
}

func TestExecuteInvalidAndOversizedResponse(t *testing.T) {
	ex, job, _ := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>oops</html>`))
	})
	if out := ex.Execute(context.Background(), job, execRun); out.Status != domain.JobStatusFailed || out.ErrorCode != "INVALID_JOB_RESPONSE" {
		t.Fatalf("non-JSON: %+v", out)
	}
	big := `{"status":"succeeded","summary":"` + strings.Repeat("x", domain.MaxResultBytes) + `"}`
	ex2, job2, _ := execFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(big)) })
	if out := ex2.Execute(context.Background(), job2, execRun); out.Status != domain.JobStatusFailed || out.ErrorCode != "INVALID_JOB_RESPONSE" {
		t.Fatalf("oversized: %+v", out)
	}
}

func TestExecuteTimeoutBecomesTimedOut(t *testing.T) {
	ex, job, _ := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		// Drain the body first: net/http only watches for client disconnect (and
		// cancels r.Context()) after the request body has been read to EOF.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	})
	job.TimeoutSeconds, job.MaxAttempts = 1, 1
	out := ex.Execute(context.Background(), job, execRun)
	if out.Status != domain.JobStatusTimedOut {
		t.Fatalf("want TIMED_OUT, got %+v", out)
	}
}

func TestExecuteUnconfiguredTarget(t *testing.T) {
	ex := NewJobExecutor(map[string]string{}, execSecret, http.DefaultClient)
	out := ex.Execute(context.Background(), domain.ScheduledJob{Code: "x", OwnerService: "ghost-service", Path: "/internal/jobs/x", TimeoutSeconds: 10, MaxAttempts: 1}, execRun)
	if out.Status != domain.JobStatusFailed || out.ErrorCode != "JOB_TARGET_NOT_CONFIGURED" {
		t.Fatalf("unexpected %+v", out)
	}
}

func TestExecuteCancelledBeforeAttemptIsTerminal(t *testing.T) {
	ex, job, calls := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"succeeded"}`))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := ex.Execute(ctx, job, execRun)
	if out.Status != domain.JobStatusFailed || out.ErrorCode != "JOB_CANCELLED" || *calls != 0 {
		t.Fatalf("unexpected outcome %+v calls=%d", out, *calls)
	}
}

func TestExecuteLongErrorCodeIsBounded(t *testing.T) {
	code := strings.Repeat("E", 100)
	ex, job, calls := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed","error_code":"` + code + `","error_message":"x"}`))
	})
	out := ex.Execute(context.Background(), job, execRun)
	if out.Status != domain.JobStatusFailed || len(out.ErrorCode) > domain.MaxErrorCodeLen || out.ErrorCode != code[:domain.MaxErrorCodeLen] || *calls != 1 {
		t.Fatalf("unexpected outcome %+v calls=%d", out, *calls)
	}
}

func TestExecuteBlankErrorCodeFallsBack(t *testing.T) {
	ex, job, _ := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed","error_code":"   "}`))
	})
	if out := ex.Execute(context.Background(), job, execRun); out.ErrorCode != "JOB_REPORTED_FAILURE" {
		t.Fatalf("unexpected outcome %+v", out)
	}
}

func TestExecutePartialBodyTimeoutBecomesTimedOut(t *testing.T) {
	ex, job, _ := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // see TestExecuteTimeoutBecomesTimedOut
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"succ`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	job.TimeoutSeconds, job.MaxAttempts = 1, 1
	out := ex.Execute(context.Background(), job, execRun)
	if out.Status != domain.JobStatusTimedOut || out.ErrorCode != "JOB_TIMEOUT" {
		t.Fatalf("want TIMED_OUT/JOB_TIMEOUT, got %+v", out)
	}
}

func TestExecuteParentCancelDuringRequestIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ex, job, calls := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	})
	out := ex.Execute(ctx, job, execRun)
	if out.Status != domain.JobStatusFailed || out.ErrorCode != "JOB_CANCELLED" || out.Attempt != 1 || *calls != 1 {
		t.Fatalf("unexpected outcome %+v calls=%d", out, *calls)
	}
}

func TestExecuteResultIsNotHTMLEscaped(t *testing.T) {
	ex, job, _ := execFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"succeeded","summary":"<a&b>"}`))
	})
	out := ex.Execute(context.Background(), job, execRun)
	if got := string(out.Result); got != `{"metrics":null,"summary":"<a&b>"}` {
		t.Fatalf("unexpected result %q", got)
	}
}
