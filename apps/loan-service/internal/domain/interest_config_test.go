package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// rec builds a valid stored configuration version; tests override one field.
func rec(id, org, from string, mod ...func(*InterestConfigRecord)) InterestConfigRecord {
	r := InterestConfigRecord{
		ID:                  id,
		TenantID:            "t1",
		ProductCode:         "SP001",
		EffectiveFrom:       from,
		InterestBaseCode:    "CSTL_02",
		InterestCalcMethod:  "CM123.001",
		InterestDateMethod:  "CM124.002",
		InterestStartMethod: "CM125.001",
		OverdueCheckCond:    "CM126.001",
		RoundNo:             1000,
		RoundType:           "CM163.001",
	}
	if org != "" {
		r.OrgCode = &org
	}
	for _, m := range mod {
		m(&r)
	}
	return r
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ie *InterestError
	if !errors.As(err, &ie) {
		t.Fatalf("error = %v, want *InterestError %s", err, code)
	}
	if ie.Code != code {
		t.Fatalf("error code = %s (%s), want %s", ie.Code, ie.Message, code)
	}
}

func TestResolveInterestConfigPicksTheVersionInForce(t *testing.T) {
	versions := []InterestConfigRecord{
		rec("t-2026-01", "", "2026-01-01"),
		rec("t-2026-06", "", "2026-06-01", func(r *InterestConfigRecord) { r.RoundNo = 100 }),
		rec("o14-2026-03", "14", "2026-03-01", func(r *InterestConfigRecord) { r.RoundNo = 500 }),
		rec("o14-2026-09", "14", "2026-09-01", func(r *InterestConfigRecord) { r.RoundNo = 1 }),
		rec("o16-2026-01", "16", "2026-01-01"),
	}
	cases := []struct {
		name    string
		org     string
		asOf    string
		wantID  string
		wantNo  int
		wantOrg string
	}{
		{"org version beats tenant-wide on the same day", "14", "2026-03-01", "o14-2026-03", 500, "14"},
		{"org version beats a newer tenant-wide one", "14", "2026-07-15", "o14-2026-03", 500, "14"},
		{"latest effective_from within the date wins", "14", "2026-09-01", "o14-2026-09", 1, "14"},
		{"day before the next version keeps the older one", "14", "2026-08-31", "o14-2026-03", 500, "14"},
		{"org without versions falls back to tenant-wide", "99", "2026-07-15", "t-2026-06", 100, ""},
		{"org whose first version is still in the future falls back to tenant-wide", "14", "2026-02-15", "t-2026-01", 1000, ""},
		{"no org given uses tenant-wide only", "", "2026-12-31", "t-2026-06", 100, ""},
		{"exact first effective day", "", "2026-01-01", "t-2026-01", 1000, ""},
		{"other org's rows are never used", "16", "2026-05-01", "o16-2026-01", 1000, "16"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveInterestConfig(versions, c.org, c.asOf)
			if err != nil {
				t.Fatalf("ResolveInterestConfig() error = %v", err)
			}
			if got.ConfigID != c.wantID || got.RoundNo != c.wantNo || got.OrgCode != c.wantOrg {
				t.Fatalf("got id=%s round_no=%d org=%q, want id=%s round_no=%d org=%q",
					got.ConfigID, got.RoundNo, got.OrgCode, c.wantID, c.wantNo, c.wantOrg)
			}
		})
	}
}

func TestResolveInterestConfigNotFound(t *testing.T) {
	versions := []InterestConfigRecord{
		rec("t-2026-06", "", "2026-06-01"),
		rec("o14-2026-03", "14", "2026-03-01"),
	}
	// Before the first version of every scope: no value is invented.
	_, err := ResolveInterestConfig(versions, "14", "2026-02-28")
	wantCode(t, err, ErrCodeProductConfigNotFound)
	// No rows at all: no implicit '%' row, no default.
	_, err = ResolveInterestConfig(nil, "14", "2026-07-01")
	wantCode(t, err, ErrCodeProductConfigNotFound)
	// Org-only versions do not serve an org without versions and without a tenant row.
	_, err = ResolveInterestConfig([]InterestConfigRecord{rec("o14", "14", "2026-01-01")}, "99", "2026-07-01")
	wantCode(t, err, ErrCodeProductConfigNotFound)
}

func TestResolveInterestConfigRejectsBadAsOf(t *testing.T) {
	versions := []InterestConfigRecord{rec("t", "", "2026-01-01")}
	for _, asOf := range []string{"", " ", "2026-1-1", "2026-13-01", "2026-02-30", "01/02/2026", "2026-01-01T00:00:00Z", "2026-01-01 "} {
		_, err := ResolveInterestConfig(versions, "", asOf)
		wantCode(t, err, ErrCodeProductConfigRequestInvalid)
	}
}

func TestResolveInterestConfigRefusesUnsupportedAndUnknownCodes(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*InterestConfigRecord)
		code string
	}{
		{"CSTL_01 has no confirmed meaning", func(r *InterestConfigRecord) { r.InterestBaseCode = "CSTL_01" }, ErrCodeInterestBaseUndefined},
		{"year basis alias CSTL_365 is not a source code", func(r *InterestConfigRecord) { r.InterestBaseCode = "CSTL_365" }, ErrCodeInterestBaseUnknown},
		{"year basis 360 is not supported", func(r *InterestConfigRecord) { r.InterestBaseCode = "CSTL_360" }, ErrCodeInterestBaseUnknown},
		{"year basis lowercase is not normalised", func(r *InterestConfigRecord) { r.InterestBaseCode = "cstl_02" }, ErrCodeInterestBaseUnknown},
		{"year basis empty", func(r *InterestConfigRecord) { r.InterestBaseCode = "" }, ErrCodeInterestBaseUnknown},
		{"CM123.002 not supported", func(r *InterestConfigRecord) { r.InterestCalcMethod = "CM123.002" }, ErrCodeInterestCalcMethodUnsupported},
		{"CM125.002 not supported", func(r *InterestConfigRecord) { r.InterestStartMethod = "CM125.002" }, ErrCodeInterestStartMethodUnsupported},
		{"CM126.002 not supported", func(r *InterestConfigRecord) { r.OverdueCheckCond = "CM126.002" }, ErrCodeInterestOverdueCheckUnsupported},
		{"CM124.0021 is not CM124.002", func(r *InterestConfigRecord) { r.InterestDateMethod = "CM124.0021" }, ErrCodeProductConfigInvalid},
		{"CM124 padded", func(r *InterestConfigRecord) { r.InterestDateMethod = "CM124.002 " }, ErrCodeProductConfigInvalid},
		{"CM124 two codes", func(r *InterestConfigRecord) { r.InterestDateMethod = "CM124.001,CM124.002" }, ErrCodeProductConfigInvalid},
		{"CM124 empty", func(r *InterestConfigRecord) { r.InterestDateMethod = "" }, ErrCodeProductConfigInvalid},
		{"CM123 unknown", func(r *InterestConfigRecord) { r.InterestCalcMethod = "CM123.0011" }, ErrCodeProductConfigInvalid},
		{"CM125 empty", func(r *InterestConfigRecord) { r.InterestStartMethod = "" }, ErrCodeProductConfigInvalid},
		{"CM126 empty", func(r *InterestConfigRecord) { r.OverdueCheckCond = "" }, ErrCodeProductConfigInvalid},
		{"round type EPAS name ROUND", func(r *InterestConfigRecord) { r.RoundType = "ROUND" }, ErrCodeProductConfigInvalid},
		{"round type CM163.0011", func(r *InterestConfigRecord) { r.RoundType = "CM163.0011" }, ErrCodeProductConfigInvalid},
		{"round type empty", func(r *InterestConfigRecord) { r.RoundType = "" }, ErrCodeProductConfigInvalid},
		{"negative round_no", func(r *InterestConfigRecord) { r.RoundNo = -1 }, ErrCodeProductConfigInvalid},
		{"negative grace days", func(r *InterestConfigRecord) { g := -1; r.PrincipalGraceDays = &g }, ErrCodeProductConfigInvalid},
		{"overdue rate type unknown", func(r *InterestConfigRecord) {
			ty, v := "CM127.002", decimal.NewFromInt(130)
			r.OverdueRateType, r.OverdueRateValue = &ty, &v
		}, ErrCodeProductConfigInvalid},
		{"overdue rate type without value", func(r *InterestConfigRecord) { ty := "CM127.001"; r.OverdueRateType = &ty }, ErrCodeProductConfigInvalid},
		{"overdue rate value without type", func(r *InterestConfigRecord) { v := decimal.NewFromInt(150); r.OverdueRateValue = &v }, ErrCodeProductConfigInvalid},
		{"negative overdue rate", func(r *InterestConfigRecord) {
			ty, v := "CM127.001", decimal.NewFromInt(-1)
			r.OverdueRateType, r.OverdueRateValue = &ty, &v
		}, ErrCodeProductConfigInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ResolveInterestConfig([]InterestConfigRecord{rec("v", "", "2026-01-01", c.mod)}, "", "2026-06-01")
			wantCode(t, err, c.code)
		})
	}
}

// A newer version with a bad code must not be skipped in favour of an older
// good one: the version in force is picked first, then validated.
func TestResolveInterestConfigDoesNotFallBackPastARejectedVersion(t *testing.T) {
	versions := []InterestConfigRecord{
		rec("old-good", "", "2026-01-01"),
		rec("new-bad", "", "2026-05-01", func(r *InterestConfigRecord) { r.InterestBaseCode = "CSTL_01" }),
	}
	_, err := ResolveInterestConfig(versions, "", "2026-06-01")
	wantCode(t, err, ErrCodeInterestBaseUndefined)
	got, err := ResolveInterestConfig(versions, "", "2026-04-30")
	if err != nil || got.ConfigID != "old-good" {
		t.Fatalf("before the bad version: %v, %v", got, err)
	}
	// Same for scopes: an org version in force that is unsupported does not
	// silently fall back to the tenant-wide one.
	scoped := []InterestConfigRecord{
		rec("tenant-good", "", "2026-01-01"),
		rec("org-bad", "14", "2026-01-01", func(r *InterestConfigRecord) { r.InterestCalcMethod = "CM123.002" }),
	}
	_, err = ResolveInterestConfig(scoped, "14", "2026-06-01")
	wantCode(t, err, ErrCodeInterestCalcMethodUnsupported)
}

func TestResolveInterestConfigRefusesAmbiguousVersions(t *testing.T) {
	dup := []InterestConfigRecord{rec("a", "14", "2026-01-01"), rec("b", "14", "2026-01-01")}
	_, err := ResolveInterestConfig(dup, "14", "2026-06-01")
	wantCode(t, err, ErrCodeProductConfigInvalid)
	dupTenant := []InterestConfigRecord{rec("a", "", "2026-01-01"), rec("b", "", "2026-01-01")}
	_, err = ResolveInterestConfig(dupTenant, "", "2026-06-01")
	wantCode(t, err, ErrCodeProductConfigInvalid)
	// A bad stored effective_from is a data error, not a skipped row.
	_, err = ResolveInterestConfig([]InterestConfigRecord{rec("a", "", "2026/01/01")}, "", "2026-06-01")
	wantCode(t, err, ErrCodeProductConfigInvalid)
}

func TestParseInterestConfigMapsEverySupportedValue(t *testing.T) {
	grace := 10
	ty, v := "CM127.001", decimal.RequireFromString("130")
	got, err := ParseInterestConfig(rec("v1", "14", "2026-01-01", func(r *InterestConfigRecord) {
		r.InterestDateMethod = "CM124.001"
		r.RoundNo = 0
		r.RoundType = "CM163.003"
		r.PrincipalGraceDays = &grace
		r.OverdueRateType, r.OverdueRateValue = &ty, &v
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigID != "v1" || got.OrgCode != "14" || got.EffectiveFrom != "2026-01-01" {
		t.Fatalf("identity = %+v", got)
	}
	if got.BaseCode != "CSTL_02" || got.YearDays != 365 {
		t.Fatalf("year basis = %s/%d, want CSTL_02/365", got.BaseCode, got.YearDays)
	}
	if got.DateMethod != DateMethodSkipFirstDay || got.RoundNo != 0 || got.RoundType != RoundModeFloor {
		t.Fatalf("calc flags = %+v", got)
	}
	days, err := got.GraceDays()
	if err != nil || days != 10 {
		t.Fatalf("GraceDays() = %d, %v", days, err)
	}
	rate, err := got.OverdueRate()
	if err != nil || !rate.Equal(decimal.NewFromInt(130)) {
		t.Fatalf("OverdueRate() = %s, %v", rate, err)
	}
}

func TestInterestConfigUnconfirmedValuesAreErrorsNotZero(t *testing.T) {
	got, err := ParseInterestConfig(rec("v1", "", "2026-01-01"))
	if err != nil {
		t.Fatal(err)
	}
	if got.PrincipalGraceDays != nil || got.OverdueRatePercent != nil {
		t.Fatalf("unconfirmed values were filled in: %+v", got)
	}
	_, err = got.GraceDays()
	wantCode(t, err, ErrCodeProductConfigGraceDaysMissing)
	_, err = got.OverdueRate()
	wantCode(t, err, ErrCodeProductConfigOverdueRateMissing)
}

func TestInterestConfigFeedsTheCalculator(t *testing.T) {
	cfg, err := ParseInterestConfig(rec("v1", "", "2026-01-01", func(r *InterestConfigRecord) { r.RoundNo = 1000 }))
	if err != nil {
		t.Fatal(err)
	}
	rate := decimal.NewFromInt(12)
	in := InterestInput{
		From:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:            time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
		Balances:      []BalanceEntry{{ValueDate: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), Balance: decimal.NewFromInt(100_000_000)}},
		AgreementRate: &rate,
	}
	cfg.ApplyTo(&in)
	if in.BaseCode != "CSTL_02" || in.DateMethod != "CM124.002" || in.RoundNo != 1000 || in.RoundType != "CM163.001" {
		t.Fatalf("ApplyTo left input = %+v", in)
	}
	res, err := CalculateInterest(in)
	if err != nil {
		t.Fatalf("CalculateInterest() with a resolved config: %v", err)
	}
	// 100,000,000 x 12% x 30 / 365 = 986,301.37, rounded once to 1,000.
	if !res.Total.Equal(decimal.NewFromInt(986000)) {
		t.Fatalf("Total = %s, want 986000", res.Total)
	}
}
