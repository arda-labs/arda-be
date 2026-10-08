package service

import (
	"context"
	"errors"
	"testing"
	"time"

	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
)

type eodCalendarStub struct {
	date      time.Time
	dateErr   error
	lastScope ardaBusinessDate.Scope
}

func (c *eodCalendarStub) CurrentBusinessDate(_ context.Context, scope ardaBusinessDate.Scope) (time.Time, error) {
	c.lastScope = scope
	return c.date, c.dateErr
}

func (*eodCalendarStub) IsHoliday(context.Context, ardaBusinessDate.Scope, time.Time) (bool, error) {
	return false, nil
}

func TestResolveEODBusinessDateUsesPlatformCalendar(t *testing.T) {
	calendar := &eodCalendarStub{date: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	got, err := resolveEODBusinessDate(context.Background(), calendar, "tenant-a", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-10-08" {
		t.Fatalf("resolved business date = %q", got)
	}
	if calendar.lastScope.Type != ardaBusinessDate.ScopeSystem || calendar.lastScope.TenantID != "tenant-a" {
		t.Fatalf("calendar scope = %+v", calendar.lastScope)
	}
}

func TestResolveEODBusinessDatePreservesExplicitDate(t *testing.T) {
	got, err := resolveEODBusinessDate(context.Background(), nil, "tenant-a", "2026-10-01")
	if err != nil || got != "2026-10-01" {
		t.Fatalf("explicit business date = %q, %v", got, err)
	}
}

func TestResolveEODBusinessDateFailsClosed(t *testing.T) {
	calendar := &eodCalendarStub{dateErr: errors.New("Platform unavailable")}
	if _, err := resolveEODBusinessDate(context.Background(), calendar, "tenant-a", ""); err == nil {
		t.Fatal("expected Platform calendar failure")
	}
	if _, err := resolveEODBusinessDate(context.Background(), nil, "tenant-a", ""); err == nil {
		t.Fatal("expected missing Platform calendar failure")
	}
	if _, err := resolveEODBusinessDate(context.Background(), &eodCalendarStub{}, "", ""); err == nil {
		t.Fatal("expected tenant validation failure")
	}
}

func TestResolveEODBusinessDateRejectsInvalidExplicitDate(t *testing.T) {
	if _, err := resolveEODBusinessDate(context.Background(), nil, "tenant-a", "2026-2-8"); err == nil {
		t.Fatal("expected non-canonical date to be rejected")
	}
}
