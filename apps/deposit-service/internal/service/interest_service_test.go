package service

import "testing"

func TestAccrualWindow(t *testing.T) {
	tests := []struct {
		name         string
		openDate     string
		lastPosted   string
		businessDate string
		wantFrom     string
		wantDays     int
		wantOK       bool
	}{
		{name: "first run accrues from open date", openDate: "2026-01-01", businessDate: "2026-01-03", wantFrom: "2026-01-01", wantDays: 3, wantOK: true},
		{name: "single day", openDate: "2026-01-05", businessDate: "2026-01-05", wantFrom: "2026-01-05", wantDays: 1, wantOK: true},
		{name: "continues after last posted period", openDate: "2026-01-01", lastPosted: "2026-01-03", businessDate: "2026-01-05", wantFrom: "2026-01-04", wantDays: 2, wantOK: true},
		{name: "nothing to accrue when business date already covered", openDate: "2026-01-01", lastPosted: "2026-01-05", businessDate: "2026-01-05", wantOK: false},
		{name: "nothing to accrue when last posted is ahead of business date", openDate: "2026-01-01", lastPosted: "2026-01-06", businessDate: "2026-01-05", wantOK: false},
		{name: "business date before open date", openDate: "2026-01-10", businessDate: "2026-01-05", wantOK: false},
		{name: "unparseable last posted period is skipped", openDate: "2026-01-01", lastPosted: "not-a-date", businessDate: "2026-01-05", wantOK: false},
		{name: "unparseable business date is skipped", openDate: "2026-01-01", businessDate: "05/01/2026", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, days, ok := accrualWindow(tt.openDate, tt.lastPosted, tt.businessDate)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if from != tt.wantFrom || days != tt.wantDays {
				t.Fatalf("got (%s, %d), want (%s, %d)", from, days, tt.wantFrom, tt.wantDays)
			}
		})
	}
}

// TestAccrualWindowRetriesFailedDay pins the fix for the lost-interest bug: the
// day whose accrual is still PENDING is not part of LastAccrualPeriod, so the
// next run starts on it again instead of skipping it.
func TestAccrualWindowRetriesFailedDay(t *testing.T) {
	// Last POSTED period is 2026-01-09 even though 2026-01-10 was staged
	// PENDING by the failed run; the window must include 2026-01-10 again.
	from, days, ok := accrualWindow("2026-01-01", "2026-01-09", "2026-01-10")
	if !ok || from != "2026-01-10" || days != 1 {
		t.Fatalf("failed day must be retried, got (%s, %d, %v)", from, days, ok)
	}
}

func TestResolveOpMode(t *testing.T) {
	tests := []struct {
		status string
		want   opMode
	}{
		{status: "SUBMITTED", want: opModeFresh},
		{status: "POSTING", want: opModeResume},
		{status: "POSTED", want: opModeSettled},
		{status: "REJECTED", want: opModeInvalid},
		{status: "DRAFT", want: opModeInvalid},
		{status: "", want: opModeInvalid},
	}
	for _, tt := range tests {
		if got := resolveOpMode(tt.status); got != tt.want {
			t.Fatalf("resolveOpMode(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// TestInterestOpStateMachine documents that a resumed POSTING op must not run
// the fresh reserve step again (that would decrement accrued twice), while a
// POSTED op is a no-op and a rejected/draft one is invalid.
func TestInterestOpStateMachine(t *testing.T) {
	if resolveOpMode("POSTING") == opModeFresh {
		t.Fatal("POSTING must resume after the reserve, not reserve again")
	}
	if resolveOpMode("POSTED") == opModeFresh {
		t.Fatal("POSTED must be an idempotent no-op")
	}
}
