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
func ValidateSchedule(expr, tz string) error {
	if _, err := loadLocation(tz); err != nil {
		return err
	}
	_, err := parse(expr)
	return err
}

// NextRun returns the first activation strictly after `after`, evaluated in tz.
func NextRun(expr, tz string, after time.Time) (time.Time, error) {
	loc, err := loadLocation(tz)
	if err != nil {
		return time.Time{}, err
	}
	s, err := parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	return s.Next(after.In(loc)).UTC(), nil
}

// PreviewRuns returns the next n activations after `after`.
func PreviewRuns(expr, tz string, after time.Time, n int) ([]time.Time, error) {
	out := make([]time.Time, 0, n)
	cursor := after
	for i := 0; i < n; i++ {
		next, err := NextRun(expr, tz, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, next)
		cursor = next
	}
	return out, nil
}
