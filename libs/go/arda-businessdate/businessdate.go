// Package businessdate resolves financial dates from the Platform calendar.
// It deliberately has no clock fallback: a missing source is an operational
// error, not permission to use a container's current date.
package businessdate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ScopeType string

const (
	ScopeSystem ScopeType = "SYSTEM"
	ScopeOrg    ScopeType = "ORG"
)

type Scope struct {
	TenantID string
	Type     ScopeType
	OrgCode  string
}

type Calendar interface {
	CurrentBusinessDate(context.Context, Scope) (time.Time, error)
	IsHoliday(context.Context, Scope, time.Time) (bool, error)
}

func validateScope(scope Scope) error {
	if strings.TrimSpace(scope.TenantID) == "" {
		if scope.Type != ScopeSystem {
			return errors.New("business-date tenant is required for ORG scope")
		}
	}
	switch scope.Type {
	case ScopeSystem:
		if strings.TrimSpace(scope.OrgCode) != "" {
			return errors.New("SYSTEM business-date scope must not include an org code")
		}
	case ScopeOrg:
		if strings.TrimSpace(scope.OrgCode) == "" {
			return errors.New("ORG business-date scope requires an org code")
		}
	default:
		return fmt.Errorf("unsupported business-date scope %q", scope.Type)
	}
	return nil
}

func ValidateScope(scope Scope) error { return validateScope(scope) }

func BusinessDate(ctx context.Context, calendar Calendar, scope Scope) (time.Time, error) {
	if calendar == nil {
		return time.Time{}, errors.New("business-date calendar is required")
	}
	if err := validateScope(scope); err != nil {
		return time.Time{}, err
	}
	date, err := calendar.CurrentBusinessDate(ctx, scope)
	if err != nil {
		return time.Time{}, fmt.Errorf("resolve current business date: %w", err)
	}
	if date.IsZero() {
		return time.Time{}, errors.New("Platform returned an empty business date")
	}
	return dateOnly(date), nil
}

func IsWorkingDay(ctx context.Context, calendar Calendar, scope Scope, date time.Time) (bool, error) {
	if calendar == nil {
		return false, errors.New("business-date calendar is required")
	}
	if err := validateScope(scope); err != nil {
		return false, err
	}
	date = dateOnly(date)
	if date.IsZero() {
		return false, errors.New("working-day date is required")
	}
	if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
		return false, nil
	}
	holiday, err := calendar.IsHoliday(ctx, scope, date)
	if err != nil {
		return false, fmt.Errorf("check Platform holiday: %w", err)
	}
	return !holiday, nil
}

// NextWorkingDay returns the first working date strictly after date.
func NextWorkingDay(ctx context.Context, calendar Calendar, scope Scope, date time.Time) (time.Time, error) {
	if date.IsZero() {
		return time.Time{}, errors.New("starting business date is required")
	}
	candidate := dateOnly(date).AddDate(0, 0, 1)
	for i := 0; i < 370; i++ {
		working, err := IsWorkingDay(ctx, calendar, scope, candidate)
		if err != nil {
			return time.Time{}, err
		}
		if working {
			return candidate, nil
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	return time.Time{}, errors.New("no working day found within 370 calendar days")
}

// AddWorkingDays moves from date by count working days, excluding the start
// date. Zero returns the normalized start date.
func AddWorkingDays(ctx context.Context, calendar Calendar, scope Scope, date time.Time, count int) (time.Time, error) {
	if date.IsZero() {
		return time.Time{}, errors.New("starting business date is required")
	}
	if count < 0 {
		return time.Time{}, errors.New("working-day count must not be negative")
	}
	result := dateOnly(date)
	for moved := 0; moved < count; {
		next, err := NextWorkingDay(ctx, calendar, scope, result)
		if err != nil {
			return time.Time{}, err
		}
		result = next
		moved++
	}
	return result, nil
}

func dateOnly(date time.Time) time.Time {
	if date.IsZero() {
		return time.Time{}
	}
	year, month, day := date.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
