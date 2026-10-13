package domain

import (
	"errors"
	"testing"
	"time"
)

func TestValidateSchedule(t *testing.T) {
	cases := []struct {
		name, expr, tz string
		want           error
	}{
		{"daily", "0 6 * * *", "Asia/Ho_Chi_Minh", nil},
		{"too few fields", "* * *", "Asia/Ho_Chi_Minh", ErrInvalidCron},
		{"seconds field rejected", "0 0 6 * * *", "Asia/Ho_Chi_Minh", ErrInvalidCron},
		{"bad range", "61 * * * *", "Asia/Ho_Chi_Minh", ErrInvalidCron},
		{"unknown tz", "0 6 * * *", "Asia/Hanoi", ErrInvalidTimezone},
		{"empty tz", "0 6 * * *", "", ErrInvalidTimezone},
		{"TZ prefix smuggling", "CRON_TZ=UTC 0 6 * * *", "Asia/Ho_Chi_Minh", ErrInvalidCron},
		{"never fires feb 30", "0 0 30 2 *", "Asia/Ho_Chi_Minh", ErrInvalidCron},
		{"never fires apr 31", "0 0 31 4 *", "Asia/Ho_Chi_Minh", ErrInvalidCron},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchedule(tc.expr, tc.tz)
			if tc.want == nil && err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestNextRunUsesTimezone(t *testing.T) {
	// 2026-10-11 23:30 UTC = 2026-10-12 06:30 ICT, so the next 06:00 ICT is 2026-10-13 06:00 ICT = 2026-10-12 23:00 UTC.
	after := time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC)
	got, err := NextRun("0 6 * * *", "Asia/Ho_Chi_Minh", after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 12, 23, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("want %s, got %s", want, got.UTC())
	}
}

func TestPreviewRunsReturnsAscendingTimes(t *testing.T) {
	after := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	runs, err := PreviewRuns("*/15 * * * *", "UTC", after, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 5 || !runs[0].Equal(after.Add(15*time.Minute)) || !runs[4].Equal(after.Add(75*time.Minute)) {
		t.Fatalf("unexpected preview %v", runs)
	}
}

func TestNextRunRejectsScheduleThatNeverFires(t *testing.T) {
	got, err := NextRun("0 0 30 2 *", "UTC", time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrInvalidCron) {
		t.Fatalf("want ErrInvalidCron, got %v (time %s)", err, got)
	}
	if !got.IsZero() {
		t.Fatalf("want zero time on error, got %s", got)
	}
	if _, err := PreviewRuns("0 0 30 2 *", "UTC", time.Now(), 3); !errors.Is(err, ErrInvalidCron) {
		t.Fatalf("PreviewRuns: want ErrInvalidCron, got %v", err)
	}
}

func TestPreviewRunsNonPositiveCount(t *testing.T) {
	for _, n := range []int{0, -1} {
		runs, err := PreviewRuns("0 6 * * *", "UTC", time.Now(), n)
		if err != nil {
			t.Fatalf("n=%d: unexpected error %v", n, err)
		}
		if len(runs) != 0 {
			t.Fatalf("n=%d: want empty slice, got %v", n, runs)
		}
	}
}
