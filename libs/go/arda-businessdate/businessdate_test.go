package businessdate

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCalendar struct {
	date     time.Time
	holidays map[string]bool
	err      error
}

func (f fakeCalendar) CurrentBusinessDate(context.Context, Scope) (time.Time, error) {
	return f.date, f.err
}
func (f fakeCalendar) IsHoliday(_ context.Context, _ Scope, date time.Time) (bool, error) {
	return f.holidays[date.Format("2006-01-02")], f.err
}

func TestBusinessDateRequiresCanonicalSourceAndScope(t *testing.T) {
	day := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	got, err := BusinessDate(context.Background(), fakeCalendar{date: day}, Scope{TenantID: "tenant-1", Type: ScopeSystem})
	if err != nil || !got.Equal(day) {
		t.Fatalf("BusinessDate() = %v, %v", got, err)
	}
	if _, err := BusinessDate(context.Background(), nil, Scope{TenantID: "tenant-1", Type: ScopeSystem}); err == nil {
		t.Fatal("missing resolver must fail; there is no wall-clock fallback")
	}
	if _, err := BusinessDate(context.Background(), fakeCalendar{err: errors.New("platform unavailable")}, Scope{TenantID: "tenant-1", Type: ScopeSystem}); err == nil {
		t.Fatal("resolver failure must be returned")
	}
	if _, err := BusinessDate(context.Background(), fakeCalendar{date: day}, Scope{TenantID: "tenant-1", Type: ScopeOrg}); err == nil {
		t.Fatal("ORG scope without org code must fail")
	}
}

func TestWorkingDayOperationsUseVersionedCalendarResolver(t *testing.T) {
	scope := Scope{TenantID: "tenant-1", Type: ScopeSystem}
	calendar := fakeCalendar{holidays: map[string]bool{"2026-07-06": true}}
	friday := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	working, err := IsWorkingDay(context.Background(), calendar, scope, friday)
	if err != nil || !working {
		t.Fatalf("Friday working=%v err=%v", working, err)
	}
	weekend, err := IsWorkingDay(context.Background(), calendar, scope, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC))
	if err != nil || weekend {
		t.Fatalf("Saturday working=%v err=%v", weekend, err)
	}
	got, err := NextWorkingDay(context.Background(), calendar, scope, friday)
	if err != nil || got.Format("2006-01-02") != "2026-07-07" {
		t.Fatalf("next day=%s err=%v", got.Format("2006-01-02"), err)
	}
	got, err = AddWorkingDays(context.Background(), calendar, scope, friday, 2)
	if err != nil || got.Format("2006-01-02") != "2026-07-08" {
		t.Fatalf("add days=%s err=%v", got.Format("2006-01-02"), err)
	}
}
