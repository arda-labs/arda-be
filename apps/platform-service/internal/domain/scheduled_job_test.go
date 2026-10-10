package domain

import (
	"errors"
	"testing"
)

func TestTransition(t *testing.T) {
	cases := []struct {
		prev, next, subject string
		ok                  bool
	}{
		{"", JobStatusFailed, "arda.platform.job.failed.v1", true},
		{"", JobStatusTimedOut, "arda.platform.job.failed.v1", true},
		{JobStatusSucceeded, JobStatusFailed, "arda.platform.job.failed.v1", true},
		{JobStatusFailed, JobStatusFailed, "", false},
		{JobStatusFailed, JobStatusTimedOut, "", false},
		{JobStatusTimedOut, JobStatusSucceeded, "arda.platform.job.recovered.v1", true},
		{JobStatusFailed, JobStatusSucceeded, "arda.platform.job.recovered.v1", true},
		{"", JobStatusSucceeded, "", false},
		{JobStatusSucceeded, JobStatusSucceeded, "", false},
		{JobStatusFailed, JobStatusSkipped, "", false},
	}
	for _, tc := range cases {
		subject, _, ok := Transition(tc.prev, tc.next)
		if ok != tc.ok || subject != tc.subject {
			t.Fatalf("Transition(%q,%q) = %q,%v want %q,%v", tc.prev, tc.next, subject, ok, tc.subject, tc.ok)
		}
	}
}

func TestJobConfigUpdateValidate(t *testing.T) {
	cron := "0 6 * * *"
	ok := JobConfigUpdate{CronExpr: &cron, Timezone: "Asia/Ho_Chi_Minh", TimeoutSeconds: 600, MaxAttempts: 2, RetryBackoffSeconds: 120, Version: 1}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	bad := ok
	bad.TimeoutSeconds = 5
	if err := bad.Validate(); !errors.Is(err, ErrFieldOutOfRange) {
		t.Fatalf("want ErrFieldOutOfRange, got %v", err)
	}
	bad = ok
	bad.MaxAttempts = 6
	if err := bad.Validate(); !errors.Is(err, ErrFieldOutOfRange) {
		t.Fatalf("want ErrFieldOutOfRange, got %v", err)
	}
	empty := ""
	manual := ok
	manual.CronExpr = &empty
	if err := manual.Validate(); err != nil {
		t.Fatalf("empty cron means manual-only and must be valid: %v", err)
	}
}

func TestCatalogIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range JobCatalog {
		if seen[d.Code] {
			t.Fatalf("duplicate code %s", d.Code)
		}
		seen[d.Code] = true
		if len(d.Path) < len("/internal/jobs/") || d.Path[:len("/internal/jobs/")] != "/internal/jobs/" {
			t.Fatalf("%s path must start with /internal/jobs/: %s", d.Code, d.Path)
		}
		if d.DefaultCron != "" {
			if err := ValidateSchedule(d.DefaultCron, DefaultTimezone); err != nil {
				t.Fatalf("%s default cron invalid: %v", d.Code, err)
			}
		}
	}
	for _, code := range []string{"ai-eval", "eod", "platform-runs-cleanup"} {
		if _, ok := FindJobDefinition(code); !ok {
			t.Fatalf("catalog missing %s", code)
		}
	}
}
