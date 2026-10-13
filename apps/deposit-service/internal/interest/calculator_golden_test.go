package interest

// Bảng case chuẩn — máy tính lãi tiền gửi (port EPAS DpmInterestCalculator,
// DPM.305 dự chi hằng ngày / DPM.306 tất toán).
//
// Quy tắc hiện tại:
//   - khoảng ngày [From, To] tính CẢ HAI ĐẦU (inclusive) — BUG-10 / câu hỏi Q1
//     trong arda-refactor-plan.md còn mở ([from,to) như EPAS hay [from,to]);
//     bảng này khóa hiện trạng, các case có ghi "hiện trạng";
//   - lãi = số dư × số ngày × lãi suất / 100 / cơ sở (365 mặc định, 360 hỗ trợ),
//     tính ở scale 10, cắt đoạn tại mỗi điểm thay đổi số dư (gửi thêm);
//   - làm tròn bội số roundNo theo CM163: HALF_UP / CEILING / FLOOR; roundNo
//     ≤ 0 giữ nguyên; service lấy Rounded.Round(0) ra minor unit.
// Số tiền là VND (minor unit = đồng).

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(y int, m time.Month, dd int) time.Time { return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC) }

func TestGoldenCalculate(t *testing.T) {
	type seg struct {
		days   int64
		amount int64 // Amount.Round(0)
	}
	cases := []struct {
		name        string
		in          Input
		wantDays    int64
		wantMinor   int64 // Rounded.Round(0).IntPart() như service
		wantSegment []seg
	}{
		{
			// 100tr × 31 × 6 /100 /365 = 509.589,04
			name: "hiện trạng [from,to]: kỳ 1 tháng 01/01→31/01 tính 31 ngày, 100 triệu, 6%/365 → 509.589",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 31), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 31, wantMinor: 509_589, wantSegment: []seg{{31, 509_589}},
		},
		{
			name: "12 tháng tròn 365 ngày, 5% → đúng 5 triệu",
			in: Input{From: d(2026, 1, 1), To: d(2026, 12, 31), Rate: decimal.NewFromInt(5), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 365, wantMinor: 5_000_000, wantSegment: []seg{{365, 5_000_000}},
		},
		{
			// 100tr × 366 × 5 /36500 = 5.013.698,63
			name: "năm nhuận 2028: 366 ngày trên cơ sở 365 → 5.013.699",
			in: Input{From: d(2028, 1, 1), To: d(2028, 12, 31), Rate: decimal.NewFromInt(5), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2028, 1, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 366, wantMinor: 5_013_699, wantSegment: []seg{{366, 5_013_699}},
		},
		{
			// 15 ngày × 100tr = 246.575,34 ; 16 ngày × 200tr = 526.027,40 ; tổng 772.602,74
			name: "gửi thêm 100 triệu ngày 16/01: cắt 2 đoạn 15 + 16 ngày → 772.603",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 31), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
				Points: []BalancePoint{
					{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)},
					{Date: d(2026, 1, 16), Balance: decimal.NewFromInt(200_000_000)},
				}},
			wantDays: 31, wantMinor: 772_603, wantSegment: []seg{{15, 246_575}, {16, 526_027}},
		},
		{
			name: "điểm số dư đưa vào không theo thứ tự vẫn cắt đúng",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 31), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
				Points: []BalancePoint{
					{Date: d(2026, 1, 16), Balance: decimal.NewFromInt(200_000_000)},
					{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)},
				}},
			wantDays: 31, wantMinor: 772_603, wantSegment: []seg{{15, 246_575}, {16, 526_027}},
		},
		{
			// 100tr × 10 × 6 /36500 = 164.383,56 — không có lãi suất rút trước hạn (BUG-10 mục 4)
			name: "hiện trạng, cần xác nhận nghiệp vụ: tất toán sớm ngày 10/01 chỉ cắt ngắn kỳ, vẫn lãi suất gốc → 164.384",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 10), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 10, wantMinor: 164_384, wantSegment: []seg{{10, 164_384}},
		},
		{
			name: "cơ sở 360: 360 triệu × 10 ngày × 10% /360 = 1 triệu",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 10), Rate: decimal.NewFromInt(10), BaseDenominator: 360,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(360_000_000)}}},
			wantDays: 10, wantMinor: 1_000_000, wantSegment: []seg{{10, 1_000_000}},
		},
		{
			name: "cơ sở ≤ 0 mặc định 365",
			in: Input{From: d(2026, 1, 1), To: d(2026, 12, 31), Rate: decimal.NewFromInt(5), BaseDenominator: 0,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 365, wantMinor: 5_000_000, wantSegment: []seg{{365, 5_000_000}},
		},
		{
			// 100tr × 1 × 6,5 /36500 = 17.808,22
			name: "dự chi 1 ngày (from = to), 6,5% → 17.808",
			in: Input{From: d(2026, 3, 1), To: d(2026, 3, 1), Rate: decimal.NewFromFloat(6.5), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 3, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 1, wantMinor: 17_808, wantSegment: []seg{{1, 17_808}},
		},
		{
			// 10 tỷ × 90 × 7,2 /36500 = 177.534.246,58
			name: "10 tỷ, 90 ngày, 7,2% → 177.534.247",
			in: Input{From: d(2026, 1, 1), To: d(2026, 3, 31), Rate: decimal.NewFromFloat(7.2), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(10_000_000_000)}}},
			wantDays: 90, wantMinor: 177_534_247, wantSegment: []seg{{90, 177_534_247}},
		},
		{
			// trước 16/01 chưa có số dư → đoạn đầu 0; 16 ngày × 100tr = 263.013,70
			name: "điểm số dư đầu tiên sau ngày from: đoạn trước đó lãi 0",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 31), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 16), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 31, wantMinor: 263_014, wantSegment: []seg{{15, 0}, {16, 263_014}},
		},
		{
			name: "lãi suất 0 → 0",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 31), Rate: decimal.Zero, BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 31, wantMinor: 0, wantSegment: []seg{{31, 0}},
		},
		{
			name: "số dư 0 → 0",
			in: Input{From: d(2026, 1, 1), To: d(2026, 1, 31), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.Zero}}},
			wantDays: 31, wantMinor: 0, wantSegment: []seg{{31, 0}},
		},
		{
			name: "to trước from → 0 ngày, 0 đồng, không đoạn",
			in: Input{From: d(2026, 1, 10), To: d(2026, 1, 1), Rate: decimal.NewFromInt(6), BaseDenominator: 365,
				Points: []BalancePoint{{Date: d(2026, 1, 1), Balance: decimal.NewFromInt(100_000_000)}}},
			wantDays: 0, wantMinor: 0, wantSegment: []seg{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Calculate(tc.in)
			if res.TotalDays != tc.wantDays {
				t.Fatalf("TotalDays = %d, muốn %d", res.TotalDays, tc.wantDays)
			}
			if got := res.Rounded.Round(0).IntPart(); got != tc.wantMinor {
				t.Fatalf("lãi = %d (raw %s), muốn %d", got, res.Raw, tc.wantMinor)
			}
			if !res.Raw.Equal(res.Rounded) {
				t.Fatalf("roundNo 0 phải giữ nguyên raw: %s ≠ %s", res.Raw, res.Rounded)
			}
			if len(res.Segments) != len(tc.wantSegment) {
				t.Fatalf("số đoạn = %d, muốn %d: %+v", len(res.Segments), len(tc.wantSegment), res.Segments)
			}
			var sum decimal.Decimal
			for i, want := range tc.wantSegment {
				got := res.Segments[i]
				sum = sum.Add(got.Amount)
				if got.Days != want.days || got.Amount.Round(0).IntPart() != want.amount {
					t.Fatalf("đoạn %d = %d ngày %s, muốn %d ngày %d", i+1, got.Days, got.Amount, want.days, want.amount)
				}
			}
			if !sum.Equal(res.Raw) {
				t.Fatalf("tổng các đoạn %s ≠ raw %s", sum, res.Raw)
			}
		})
	}
}

func TestGoldenApplyRound(t *testing.T) {
	amount := decimal.NewFromFloat(164383.56) // lãi 10 ngày ở case tất toán sớm
	cases := []struct {
		name    string
		roundNo int
		mode    string
		want    string
	}{
		{"không làm tròn (roundNo 0)", 0, RoundHalfUp, "164383.56"},
		{"roundNo âm giữ nguyên", -1, RoundHalfUp, "164383.56"},
		{"bội 1.000 HALF_UP: 164,38 → 164.000", 1000, RoundHalfUp, "164000"},
		{"bội 1.000 CEILING: → 165.000", 1000, RoundUp, "165000"},
		{"bội 1.000 FLOOR: → 164.000", 1000, RoundDown, "164000"},
		{"bội 100 HALF_UP: 1643,84 → 164.400", 100, RoundHalfUp, "164400"},
		{"bội 100 FLOOR: → 164.300", 100, RoundDown, "164300"},
		{"bội 1 HALF_UP: → 164.384", 1, RoundHalfUp, "164384"},
		{"bội 1 FLOOR: → 164.383", 1, RoundDown, "164383"},
		{"mã làm tròn lạ rơi về HALF_UP", 1000, "CM163.999", "164000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyRound(amount, tc.roundNo, tc.mode).String(); got != tc.want {
				t.Fatalf("ApplyRound(%s, %d, %s) = %s, muốn %s", amount, tc.roundNo, tc.mode, got, tc.want)
			}
		})
	}
	// Đúng nửa (x,5 của bội số): HALF_UP làm tròn lên, không banker's.
	if got := ApplyRound(decimal.NewFromInt(1500), 1000, RoundHalfUp).String(); got != "2000" {
		t.Fatalf("1.500 bội 1.000 HALF_UP = %s, muốn 2000", got)
	}
	if got := ApplyRound(decimal.NewFromInt(2500), 1000, RoundHalfUp).String(); got != "3000" {
		t.Fatalf("2.500 bội 1.000 HALF_UP = %s, muốn 3000", got)
	}
}
