package ardatime

import (
	"testing"
	"time"
)

func TestLocationIsHoChiMinh(t *testing.T) {
	loc := Location()
	if got, want := loc.String(), "Asia/Ho_Chi_Minh"; got != want {
		t.Fatalf("Location() = %q, want %q", got, want)
	}
	if got := loc.String(); time.FixedZone("ICT", 7*3600).String() == got {
		t.Fatalf("Location() resolved to a FixedZone fallback, want IANA tz")
	}
}

func TestIn(t *testing.T) {
	if loc, err := In(""); err != nil || loc.String() != "Asia/Ho_Chi_Minh" {
		t.Fatalf("In(\"\") = %v, %v; want default tz, nil", loc, err)
	}
	loc, err := In("Europe/Berlin")
	if err != nil || loc.String() != "Europe/Berlin" {
		t.Fatalf("In(Europe/Berlin) = %v, %v; want Berlin, nil", loc, err)
	}
	if _, err := In("Mars/Olympus"); err == nil {
		t.Fatalf("In(Mars/Olympus) = nil error; want error")
	}
	if got := InOrDefault("Mars/Olympus").String(); got != "Asia/Ho_Chi_Minh" {
		t.Fatalf("InOrDefault(bogus) = %q; want default", got)
	}
}

// Today/Now are clock-backed; assert shape and offset rather than values.
func TestTodayShape(t *testing.T) {
	today := Today()
	if len(today) != 10 || today[4] != '-' || today[7] != '-' {
		t.Fatalf("Today() = %q; want YYYY-MM-DD", today)
	}
	now := Now()
	if _, offset := now.Zone(); offset != 7*3600 {
		t.Fatalf("Now() offset = %d; want +7h", offset)
	}
}

func TestParseDayIsMidnightBusinessTz(t *testing.T) {
	day, err := ParseDay("2026-09-10")
	if err != nil {
		t.Fatalf("ParseDay: %v", err)
	}
	if h := day.Hour(); h != 0 {
		t.Fatalf("ParseDay hour = %d; want 0", h)
	}
	if _, offset := day.Zone(); offset != 7*3600 {
		t.Fatalf("ParseDay offset = %d; want +7h", offset)
	}
	if _, err := ParseDay("10/09/2026"); err == nil {
		t.Fatalf("ParseDay(10/09/2026) = nil error; want error")
	}
}

// The core contract: one VN business day is [prev 17:00Z, 17:00Z) — NOT
// the naive UTC day of the same calendar label.
func TestDayRangeUTC(t *testing.T) {
	from, to, err := DayRangeUTC("2026-07-01", Location())
	if err != nil {
		t.Fatalf("DayRangeUTC: %v", err)
	}
	wantFrom := time.Date(2026, 6, 30, 17, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 7, 1, 17, 0, 0, 0, time.UTC)
	if !from.Equal(wantFrom) {
		t.Fatalf("from = %s; want %s", from.UTC(), wantFrom)
	}
	if !to.Equal(wantTo) {
		t.Fatalf("to = %s; want %s", to.UTC(), wantTo)
	}
	if from.Compare(to) >= 0 {
		t.Fatalf("range not half-open ordered: %s >= %s", from, to)
	}
	// An instant inside the VN day must be contained; the counter-case is
	// 2026-06-30T16:59Z == 23:59 ICT Jun 30, which must fall outside the
	// Jul 1 range.
	inside := time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC) // 09:00 ICT
	if inside.Before(from) || !inside.Before(to) {
		t.Fatalf("09:00 ICT not contained in its own business day range")
	}
	prior := time.Date(2026, 6, 30, 16, 59, 0, 0, time.UTC)
	if !prior.Before(from) {
		t.Fatalf("23:59 ICT Jun 30 leaked into Jul 1 range")
	}
}

func TestDayRangeUTCInvalid(t *testing.T) {
	if _, _, err := DayRangeUTC("2026-13-40", Location()); err == nil {
		t.Fatalf("DayRangeUTC(bogus) = nil error; want error")
	}
}

func TestResolveDayRange(t *testing.T) {
	from, to, debug, err := ResolveDayRange("2026-09-01", "2026-09-08", Location())
	if err != nil {
		t.Fatalf("ResolveDayRange: %v", err)
	}
	if got := from.UTC().Format(time.RFC3339); got != "2026-08-31T17:00:00Z" {
		t.Fatalf("from = %s; want 2026-08-31T17:00:00Z", got)
	}
	// to is exclusive midnight after Sep 8 → Sep 8 17:00Z.
	if got := to.UTC().Format(time.RFC3339); got != "2026-09-08T17:00:00Z" {
		t.Fatalf("to = %s; want 2026-09-08T17:00:00Z (exclusive)", got)
	}
	if debug == "" {
		t.Fatalf("debug resolution line empty; want local+UTC rendering")
	}
	if _, _, _, err := ResolveDayRange("2026-09-01", "not-a-date", Location()); err == nil {
		t.Fatalf("ResolveDayRange bad to = nil error; want error")
	}
}
