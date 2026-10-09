package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
)

type accrualCalendar struct {
	date time.Time
	err  error
}

func (c accrualCalendar) CurrentBusinessDate(context.Context, ardaBusinessDate.Scope) (time.Time, error) {
	return c.date, c.err
}
func (c accrualCalendar) IsHoliday(context.Context, ardaBusinessDate.Scope, time.Time) (bool, error) {
	return false, nil
}

func TestAccrualMissingDateUsesPlatformBusinessDate(t *testing.T) {
	calendar := accrualCalendar{date: time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)}
	got, err := resolveAccrualToDate(context.Background(), calendar, "tenant-1", "")
	if err != nil || got != "2026-06-29" {
		t.Fatalf("default to_date = %q, %v", got, err)
	}
	got, err = resolveAccrualToDate(context.Background(), calendar, "tenant-1", "2026-07-01")
	if err != nil || got != "2026-07-01" {
		t.Fatalf("explicit to_date = %q, %v", got, err)
	}
}

func TestAccrualMissingDateDoesNotUseWallClockFallback(t *testing.T) {
	if _, err := resolveAccrualToDate(context.Background(), nil, "tenant-1", ""); err == nil {
		t.Fatal("missing Platform calendar must fail closed")
	}
	if _, err := resolveAccrualToDate(context.Background(), accrualCalendar{err: errors.New("Platform unavailable")}, "tenant-1", ""); err == nil {
		t.Fatal("Platform failure must be returned")
	}
}
