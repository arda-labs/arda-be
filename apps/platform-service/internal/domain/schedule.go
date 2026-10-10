package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var (
	ErrInvalidCron     = errors.New("invalid cron expression")
	ErrInvalidTimezone = errors.New("invalid timezone")
	ErrFieldOutOfRange = errors.New("job field out of range")
	cronParser         = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
)

func loadLocation(tz string) (*time.Location, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return nil, ErrInvalidTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidTimezone, tz)
	}
	return loc, nil
}

func parse(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	// The timezone lives in its own column; an inline CRON_TZ/TZ prefix would
	// silently override it.
	if strings.HasPrefix(expr, "CRON_TZ=") || strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "@") {
		return nil, ErrInvalidCron
	}
	s, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCron, err)
	}
	return s, nil
}

// ValidateSchedule checks a five-field cron expression and an IANA timezone.
// An expression that never fires (e.g. "0 0 30 2 *") is rejected.
func ValidateSchedule(expr, tz string) error {
	_, err := NextRun(expr, tz, time.Now())
	return err
}

func compile(expr, tz string) (cron.Schedule, *time.Location, error) {
	loc, err := loadLocation(tz)
	if err != nil {
		return nil, nil, err
	}
	s, err := parse(expr)
	if err != nil {
		return nil, nil, err
	}
	return s, loc, nil
}

// next wraps Schedule.Next, which returns the zero time when nothing matches
// within five years; that must never be treated as "due now".
func next(s cron.Schedule, loc *time.Location, after time.Time) (time.Time, error) {
	t := s.Next(after.In(loc))
	if t.IsZero() {
		return time.Time{}, fmt.Errorf("%w: schedule never fires", ErrInvalidCron)
	}
	return t.UTC(), nil
}

// NextRun returns the first activation strictly after `after`, evaluated in tz.
func NextRun(expr, tz string, after time.Time) (time.Time, error) {
	s, loc, err := compile(expr, tz)
	if err != nil {
		return time.Time{}, err
	}
	return next(s, loc, after)
}

// PreviewRuns returns the next n activations after `after`. n <= 0 yields an
// empty slice.
func PreviewRuns(expr, tz string, after time.Time, n int) ([]time.Time, error) {
	if n <= 0 {
		return []time.Time{}, nil
	}
	s, loc, err := compile(expr, tz)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, n)
	cursor := after
	for i := 0; i < n; i++ {
		t, err := next(s, loc, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		cursor = t
	}
	return out, nil
}
