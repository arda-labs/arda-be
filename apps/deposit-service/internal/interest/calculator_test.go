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
	to := day(2027, time.January, 1) // nửa mở [from, to)
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
	to := day(2026, time.February, 1) // nửa mở [from, to): 31 ngày
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
	to := day(2026, time.January, 11) // 10 days, nửa mở
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

// G2-DPM-NGAY-01 A: ngày To không tính lãi.
func TestCalculateHalfOpenExcludesEndDay(t *testing.T) {
	from := day(2026, time.January, 1)
	res := Calculate(Input{
		From: from, To: day(2026, time.January, 31), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
		Points: []BalancePoint{{Date: from, Balance: decimal.NewFromInt(100_000_000)}},
	})
	if res.TotalDays != 30 {
		t.Fatalf("total days = %d, want 30 (ngày 31/01 không tính)", res.TotalDays)
	}
	if got := res.Raw.String(); got != "493150.6849315068" && res.Raw.Round(4).String() != "493150.6849" {
		t.Fatalf("raw = %s, want 493150.6849", got)
	}
}

// Ví dụ P3 (difftest/dpm/CLUSTERS.md K03): 6% đổi sang 5% từ 15/07, kỳ 01/07→31/07.
// EPAS: 14 ngày 6% + 16 ngày 5% = 449.315,0685.
func TestCalculateSplitsAtRatePoints(t *testing.T) {
	from := day(2026, time.July, 1)
	res := Calculate(Input{
		From: from, To: day(2026, time.July, 31), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
		Points:     []BalancePoint{{Date: from, Balance: decimal.NewFromInt(100_000_000)}},
		RatePoints: []RatePoint{{Date: day(2026, time.July, 15), Rate: decimal.NewFromInt(5)}},
	})
	if res.TotalDays != 30 {
		t.Fatalf("total days = %d, want 30", res.TotalDays)
	}
	if len(res.Segments) != 2 || res.Segments[0].Days != 14 || res.Segments[1].Days != 16 {
		t.Fatalf("segments = %+v, want 14 + 16 days", res.Segments)
	}
	if !res.Segments[0].Rate.Equal(decimal.NewFromInt(6)) || !res.Segments[1].Rate.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("segment rates = %s/%s, want 6/5", res.Segments[0].Rate, res.Segments[1].Rate)
	}
	if got := res.Raw.Round(4).String(); got != "449315.0685" {
		t.Fatalf("raw = %s, want 449315.0685", res.Raw)
	}
}

func TestCalculateRatePointEdgeCases(t *testing.T) {
	from := day(2026, time.July, 1)
	base := Input{
		From: from, To: day(2026, time.July, 11), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
		Points: []BalancePoint{{Date: from, Balance: decimal.NewFromInt(100_000_000)}},
	}
	tests := []struct {
		name   string
		points []RatePoint
		want   []string // lãi suất từng đoạn
	}{
		{"mốc trước from có hiệu lực từ đầu kỳ", []RatePoint{{Date: day(2026, time.June, 1), Rate: decimal.NewFromInt(7)}}, []string{"7"}},
		{"mốc đúng ngày To (ngày không tính) bị bỏ qua", []RatePoint{{Date: day(2026, time.July, 11), Rate: decimal.NewFromInt(9)}}, []string{"6"}},
		{"mốc sau To bị bỏ qua", []RatePoint{{Date: day(2026, time.August, 1), Rate: decimal.NewFromInt(9)}}, []string{"6"}},
		{"trùng ngày: mốc đến sau thắng", []RatePoint{
			{Date: day(2026, time.July, 5), Rate: decimal.NewFromInt(7)},
			{Date: day(2026, time.July, 5), Rate: decimal.NewFromInt(8)},
		}, []string{"6", "8"}},
		{"mốc đưa vào không theo thứ tự", []RatePoint{
			{Date: day(2026, time.July, 8), Rate: decimal.NewFromInt(8)},
			{Date: day(2026, time.July, 5), Rate: decimal.NewFromInt(7)},
		}, []string{"6", "7", "8"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			in.RatePoints = tc.points
			res := Calculate(in)
			var got []string
			var days int64
			for _, sg := range res.Segments {
				got = append(got, sg.Rate.String())
				days += sg.Days
			}
			if days != 10 || res.TotalDays != 10 {
				t.Fatalf("ngày = %d/%d, want 10", days, res.TotalDays)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("lãi suất các đoạn = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("lãi suất các đoạn = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// Ca K04: lãi cách bội ROUND_NO đúng 1e-10, làm tròn phải chính xác một lần.
func TestApplyRoundIsExact(t *testing.T) {
	tests := []struct {
		amount  string
		roundNo int
		mode    string
		want    string
	}{
		{"5000.0000000001", 1000, RoundUp, "6000"},
		{"4999.9999999999", 1000, RoundDown, "4000"},
		{"499.9999999999", 1000, RoundHalfUp, "0"},
		{"10.0000000001", 10, RoundUp, "20"},
		{"100.0000000001", 100, RoundUp, "200"},
		{"500", 1000, RoundHalfUp, "1000"},
		{"-1500", 1000, RoundHalfUp, "-2000"},
	}
	for _, tc := range tests {
		got := ApplyRound(decimal.RequireFromString(tc.amount), tc.roundNo, tc.mode)
		if got.String() != tc.want {
			t.Fatalf("ApplyRound(%s, %d, %s) = %s, want %s", tc.amount, tc.roundNo, tc.mode, got, tc.want)
		}
	}
}

// Ca K05: sort.Slice không ổn định khi có ≥ 13 điểm; ngày trùng thì điểm đến sau thắng.
func TestCalculateSortIsStableForSameDayPoints(t *testing.T) {
	from := day(2026, time.February, 1)
	var points []BalancePoint
	for i := 0; i < 30; i++ {
		points = append(points, BalancePoint{Date: from.AddDate(0, 0, i%3), Balance: decimal.NewFromInt(int64(100 + i))})
	}
	// 30 điểm trên 3 ngày xen kẽ: mỗi ngày điểm đến sau cùng (127, 128, 129) phải thắng.
	res := Calculate(Input{
		From: from, To: from.AddDate(0, 0, 3), Rate: decimal.NewFromInt(36500), BaseDenominator: 365,
		Points: points,
	})
	if len(res.Segments) != 3 {
		t.Fatalf("segments = %+v, want 3", res.Segments)
	}
	for i, want := range []int64{127, 128, 129} {
		if !res.Segments[i].Balance.Equal(decimal.NewFromInt(want)) {
			t.Fatalf("đoạn %d có số dư %s, want %d", i+1, res.Segments[i].Balance, want)
		}
	}
}
