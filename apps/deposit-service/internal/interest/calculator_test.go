package interest

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestCalculateSingleSegment(t *testing.T) {
	from := day(2026, time.January, 1)
	to := day(2026, time.December, 31)
	res := Calculate(Input{
		From:            from,
		To:              to,
		Rate:            decimal.NewFromInt(5),
		BaseDenominator: 365,
		Points:          []BalancePoint{{Date: from, Balance: decimal.NewFromInt(100_000_000)}},
	})
	if res.TotalDays != 365 {
		t.Fatalf("total days = %d, want 365", res.TotalDays)
	}
	want := decimal.NewFromInt(5_000_000) // 100tr × 365 × 5% / 365
	if !res.Raw.Equal(want) {
		t.Fatalf("raw = %s, want %s", res.Raw, want)
	}
	if len(res.Segments) != 1 || res.Segments[0].Days != 365 {
		t.Fatalf("segments = %+v, want one 365-day segment", res.Segments)
	}
}

func TestCalculateSplitSegments(t *testing.T) {
	from := day(2026, time.January, 1)
	to := day(2026, time.January, 31)
	res := Calculate(Input{
		From:            from,
		To:              to,
		Rate:            decimal.NewFromInt(6),
		BaseDenominator: 365,
		Points: []BalancePoint{
			{Date: from, Balance: decimal.NewFromInt(100_000_000)},
			{Date: day(2026, time.January, 16), Balance: decimal.NewFromInt(200_000_000)},
		},
	})
	if res.TotalDays != 31 {
		t.Fatalf("total days = %d, want 31", res.TotalDays)
	}
	if len(res.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(res.Segments))
	}
	if res.Segments[0].Days != 15 || res.Segments[1].Days != 16 {
		t.Fatalf("segment days = %d/%d, want 15/16", res.Segments[0].Days, res.Segments[1].Days)
	}
	sum := res.Segments[0].Amount.Add(res.Segments[1].Amount)
	if !res.Raw.Equal(sum) {
		t.Fatalf("raw %s != segment sum %s", res.Raw, sum)
	}
	if res.Raw.LessThan(decimal.NewFromInt(700_000)) || res.Raw.GreaterThan(decimal.NewFromInt(800_000)) {
		t.Fatalf("raw = %s, out of the expected 700k..800k band", res.Raw)
	}
}

func TestFourHundredBaseDenominator(t *testing.T) {
	from := day(2026, time.January, 1)
	to := day(2026, time.January, 10) // 10 days
	res := Calculate(Input{
		From:            from,
		To:              to,
		Rate:            decimal.NewFromInt(10),
		BaseDenominator: 360,
		Points:          []BalancePoint{{Date: from, Balance: decimal.NewFromInt(360_000_000)}},
	})
	// 360tr × 10 × 10% / 360 = 1,000,000
	if !res.Raw.Equal(decimal.NewFromInt(1_000_000)) {
		t.Fatalf("raw = %s, want 1000000", res.Raw)
	}
}

func TestApplyRoundModes(t *testing.T) {
	amount := decimal.NewFromInt(12_345)
	cases := []struct {
		roundNo  int
		mode     string
		expected string
	}{
		{0, RoundHalfUp, "12345"},
		{1000, RoundHalfUp, "12000"},
		{1000, RoundUp, "13000"},
		{1000, RoundDown, "12000"},
		{100, RoundHalfUp, "12300"},
	}
	for _, tc := range cases {
		got := ApplyRound(amount, tc.roundNo, tc.mode)
		if got.String() != tc.expected {
			t.Fatalf("ApplyRound(%s, %d, %s) = %s, want %s", amount, tc.roundNo, tc.mode, got, tc.expected)
		}
	}
}
