// Package ardatime centralizes business-date handling for arda services.
//
// Convention (docs/db-schema-conventions.md):
//   - Persistence stores TIMESTAMPTZ UTC instants; anything persisted is
//     stamped with UTC (pgx returns timestamptz as UTC).
//   - Business-date concepts ("today", "this month", date ranges) resolve
//     in a timezone first — the user's IANA timezone when known, the
//     platform default (Asia/Ho_Chi_Minh) otherwise — and convert back to
//     UTC instants before touching the DB.
//   - Timestamp ranges are half-open [from, to); a date-only `to` bound is
//     exclusive at midnight of the NEXT business day.
//   - CURRENT_DATE / server-local time.Now() never define business dates:
//     containers run UTC, so both are wrong between 00:00–06:59 ICT.
package ardatime

import (
	"context"
	"fmt"
	"strings"
	"time"

	// Embed the IANA tz database so LoadLocation works in distroless/alpine
	// images (no OS tzdata) and on Windows dev machines.
	_ "time/tzdata"
)

// DefaultTimezoneName is the IANA name of the platform-default business
// timezone. Per-user timezones (iam_users.timezone) override it when set.
const DefaultTimezoneName = "Asia/Ho_Chi_Minh"

// DefaultLocale is the platform-default BCP-47 locale.
const DefaultLocale = "vi-VN"

// LayoutDate is the canonical YYYY-MM-DD business-date layout (DATE columns
// / ISO strings).
const LayoutDate = "2006-01-02"

var defaultLocation = mustLoadDefault()

func mustLoadDefault() *time.Location {
	loc, err := time.LoadLocation(DefaultTimezoneName)
	if err != nil {
		// Unreachable while time/tzdata is embedded; guards exotic builds.
		return time.FixedZone("ICT", 7*60*60)
	}
	return loc
}

// Location returns the default business timezone (Asia/Ho_Chi_Minh).
func Location() *time.Location { return defaultLocation }

// In resolves an IANA timezone name. An empty name yields the default
// business timezone; an unknown name is an error so callers can reject it
// at the validation boundary.
func In(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultLocation, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", name, err)
	}
	return loc, nil
}

// InOrDefault resolves an IANA timezone name, falling back to the default
// business timezone for empty or unknown names. Use In when unknown names
// must surface as a validation error instead.
func InOrDefault(name string) *time.Location {
	loc, err := In(name)
	if err != nil {
		return defaultLocation
	}
	return loc
}

// Now returns the current instant rendered in the default business timezone.
func Now() time.Time { return time.Now().In(defaultLocation) }

// NowIn returns the current instant rendered in loc.
func NowIn(loc *time.Location) time.Time { return time.Now().In(loc) }

// Today returns today's business date in the default timezone, YYYY-MM-DD.
func Today() string { return TodayIn(defaultLocation) }

// TodayIn returns today's date in loc, YYYY-MM-DD.
func TodayIn(loc *time.Location) string { return time.Now().In(loc).Format(LayoutDate) }

// ParseDay parses a YYYY-MM-DD string as midnight in the default business
// timezone. Use DayRangeUTC (not this) for timestamp WHERE clauses.
func ParseDay(date string) (time.Time, error) { return ParseDayIn(date, defaultLocation) }

// ParseDayIn parses a YYYY-MM-DD string as midnight in loc.
func ParseDayIn(date string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation(LayoutDate, date, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q: %w", date, err)
	}
	return t, nil
}

// DayRangeUTC converts one business date in loc to the half-open UTC instant
// range [from, to) covering exactly that calendar day — the shape timestamp
// WHERE clauses consume. 2026-07-01 in Asia/Ho_Chi_Minh resolves to
// [2026-06-30T17:00:00Z, 2026-07-01T17:00:00Z).
func DayRangeUTC(day string, loc *time.Location) (from, to time.Time, err error) {
	local, err := ParseDayIn(day, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return local.UTC(), local.AddDate(0, 0, 1).UTC(), nil
}

// DayRangeUTCIn is DayRangeUTC for the default business timezone.
func DayRangeUTCIn(day string) (from, to time.Time, err error) {
	return DayRangeUTC(day, defaultLocation)
}

// ResolveDayRange converts a client-supplied date-only [fromStr, toStr]
// window (inclusive both ends as users understand "from Sep 1 to Sep 8")
// into the half-open UTC instant range [from, to+1day) in loc. Empty bounds
// come back as zero times — callers decide their own default. The second
// return is a human-readable resolution line for request logs, per the
// convention that range resolution surfaces both the local wall time and
// the UTC instants.
func ResolveDayRange(fromStr, toStr string, loc *time.Location) (from, to time.Time, debug string, err error) {
	from, err = ParseDayIn(fromStr, loc)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	to, err = ParseDayIn(toStr, loc)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	to = to.AddDate(0, 0, 1)
	debug = fmt.Sprintf("range %s..%s %s → [%s, %s) UTC",
		fromStr, toStr, loc.String(), from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	return from.UTC(), to.UTC(), debug, nil
}

// AddMonthsClamped shifts a YYYY-MM-DD business date by whole months,
// clamping the day-of-month to the target month's last day — banking
// maturity semantics: Jan 31 + 1 month = Feb 28/29, never Mar 2/3. Note
// that time.AddDate alone normalizes overflow instead of clamping.
func AddMonthsClamped(day string, months int) (string, error) {
	t, err := ParseDay(day)
	if err != nil {
		return "", err
	}
	target := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, months, 0)
	lastDay := time.Date(target.Year(), target.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	clamped := t.Day()
	if clamped > lastDay {
		clamped = lastDay
	}
	return time.Date(target.Year(), target.Month(), clamped, 0, 0, 0, 0, t.Location()).Format(LayoutDate), nil
}

// ── Per-request timezone context ──
// The BFF injects the session user's IANA timezone as the X-User-Timezone
// header; the shared HTTP middleware resolves it into a *time.Location and
// stores it in the request context. Handlers/services then resolve
// business dates per user instead of the tenant default.

type tzKey struct{}

// WithTZ returns a context carrying loc as the request's business timezone.
func WithTZ(ctx context.Context, loc *time.Location) context.Context {
	return context.WithValue(ctx, tzKey{}, loc)
}

// TZ returns the request's business timezone, falling back to the platform
// default when the context carries none (internal jobs, direct calls).
func TZ(ctx context.Context) *time.Location {
	if ctx != nil {
		if loc, ok := ctx.Value(tzKey{}).(*time.Location); ok && loc != nil {
			return loc
		}
	}
	return defaultLocation
}

// TodayCtx is Today resolved in the request's business timezone.
func TodayCtx(ctx context.Context) string { return TodayIn(TZ(ctx)) }

// NowCtx is the current instant rendered in the request's business timezone.
func NowCtx(ctx context.Context) time.Time { return NowIn(TZ(ctx)) }
