package service

// Bảng case chuẩn của dự chi lũy kế (RunDaily phương án B, chủ hệ thống chốt 2026-10-18):
// số dự chi ngày d = làm tròn(lãi lũy kế từ đầu kỳ hiện hành đến hết ngày d, theo ROUND_NO + CM163 của sản phẩm)
// − tổng đã dự chi của kỳ. Hàm thuần: không DB, không finance; RunDaily chỉ nạp đầu vào rồi ghi dòng.

import (
	"errors"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/interest"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/shopspring/decimal"
)

func cd(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func intp(v int) *int { return &v }

func strp(v string) *string { return &v }

func baseCumulative(principal int64) cumulativeAccrualInput {
	return cumulativeAccrualInput{
		PeriodStart:      cd(2026, 1, 1),
		BusinessDate:     cd(2026, 1, 1),
		CurrentPrincipal: decimal.NewFromInt(principal),
		Rate:             decimal.NewFromInt(6),
		Denominator:      365,
		RoundNo:          intp(1000),
		RoundType:        strp(interest.RoundHalfUp),
	}
}

// simulatePeriod chạy từng ngày của kỳ như RunDaily: số âm hoặc 0 bị bỏ (không dòng, không bút toán âm), số dương được ghi
// và cộng vào "đã dự chi" của ngày sau. Trả về số từng ngày (0 cho ngày bỏ) và tổng đã ghi.
func simulatePeriod(t *testing.T, in cumulativeAccrualInput, days int, prior int64) ([]int64, int64) {
	t.Helper()
	amounts := make([]int64, 0, days)
	for i := 0; i < days; i++ {
		in.BusinessDate = in.PeriodStart.AddDate(0, 0, i)
		in.PriorAccrued = prior
		res, err := computeCumulativeAccrual(in)
		if err != nil {
			t.Fatalf("ngày %d: %v", i+1, err)
		}
		amount := res.Amount
		if amount <= 0 {
			amount = 0
		}
		amounts = append(amounts, amount)
		prior += amount
	}
	return amounts, prior
}

func sum(xs []int64) (total int64) {
	for _, x := range xs {
		total += x
	}
	return total
}

func TestCumulativeAccrualBriefExamples(t *testing.T) {
	tests := []struct {
		name      string
		principal int64
		wantTotal int64
		wantHead  []int64
	}{
		{"100 triệu, 30 ngày", 100_000_000, 493_000, []int64{16_000, 17_000, 16_000, 17_000}},
		{"10 triệu, 30 ngày", 10_000_000, 49_000, []int64{2_000, 1_000, 2_000, 2_000}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			amounts, total := simulatePeriod(t, baseCumulative(tc.principal), 30, 0)
			if total != tc.wantTotal {
				t.Fatalf("tổng 30 ngày = %d, muốn %d (bằng lãi một lần cho cả kỳ)", total, tc.wantTotal)
			}
			for i, want := range tc.wantHead {
				if amounts[i] != want {
					t.Fatalf("các ngày đầu = %v, muốn %v", amounts[:len(tc.wantHead)], tc.wantHead)
				}
			}
			for i, a := range amounts {
				if a%1000 != 0 {
					t.Fatalf("ngày %d dự chi %d không là bội số ROUND_NO", i+1, a)
				}
			}
		})
	}
}

// Số dư nhỏ: nhiều ngày bằng 0, nhưng tổng cả kỳ vẫn bằng lãi một lần.
func TestCumulativeAccrualSmallBalanceHasZeroDays(t *testing.T) {
	amounts, total := simulatePeriod(t, baseCumulative(1_000_000), 30, 0)
	zeros := 0
	for _, a := range amounts {
		if a == 0 {
			zeros++
		}
	}
	if zeros == 0 {
		t.Fatalf("1 triệu với bước 1.000 phải có ngày dự chi 0, nhận %v", amounts)
	}
	if total != 5_000 { // 1tr × 30 × 6 /36500 = 4.931,51 → 5.000
		t.Fatalf("tổng = %d, muốn 5000", total)
	}
}

// Ngày đầu chạy theo B sau khi sổ đã dự chi bằng làm tròn đồng mỗi ngày: giữ nguyên số đã ghi, ngày đó tự bù chênh.
func TestCumulativeAccrualAbsorbsLegacyPerDayRounding(t *testing.T) {
	in := baseCumulative(100_000_000)
	in.BusinessDate = cd(2026, 1, 11) // ngày thứ 11 của kỳ
	in.PriorAccrued = 10 * 16_438     // 10 ngày cũ, mỗi ngày làm tròn đồng
	res, err := computeCumulativeAccrual(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 16_620 { // làm tròn(180.821,92) = 181.000, trừ 164.380 đã ghi
		t.Fatalf("ngày đầu bù = %d, muốn 16620", res.Amount)
	}
	in.BusinessDate = cd(2026, 1, 30)
	in.PriorAccrued = 29 * 16_438
	if res, err = computeCumulativeAccrual(in); err != nil || res.Amount != 16_298 {
		t.Fatalf("ngày 30 = %d, %v; muốn 16298 (tổng 493.000)", res.Amount, err)
	}
}

// Số cũ đã ghi vượt lãi lũy kế làm tròn: hàm trả số âm, RunDaily bỏ ngày đó (không bút toán âm), ngày sau bù.
func TestCumulativeAccrualNegativeDaySkippedThenCaughtUp(t *testing.T) {
	in := baseCumulative(10_000_000)
	in.BusinessDate = cd(2026, 1, 5) // lãi lũy kế 8.219,18 → 8.000
	in.PriorAccrued = 8_500
	res, err := computeCumulativeAccrual(in)
	if err != nil || res.Amount != -500 {
		t.Fatalf("ngày số âm = %d, %v; muốn -500", res.Amount, err)
	}
	amounts, total := simulatePeriod(t, in, 6, 8_500) // sáu ngày đầu kỳ nhưng đã ghi sẵn 8.500
	for i := 0; i < 5; i++ {
		if amounts[i] != 0 {
			t.Fatalf("ngày %d phải bị bỏ (số âm hoặc 0), nhận %d", i+1, amounts[i])
		}
	}
	if amounts[5] != 1_500 || total != 10_000 { // ngày 6: 9.863,01 → 10.000, trừ 8.500
		t.Fatalf("ngày sau bù = %d (tổng đã ghi %d), muốn 1500 và 10000", amounts[5], total)
	}
}

// Chạy lại cùng ngày: dòng của ngày đó đã nằm trong "đã dự chi" nên số dự chi thêm là 0, không dự chi hai lần.
func TestCumulativeAccrualIsIdempotentForSameDay(t *testing.T) {
	in := baseCumulative(100_000_000)
	in.BusinessDate = cd(2026, 1, 3)
	first, err := computeCumulativeAccrual(in)
	if err != nil {
		t.Fatal(err)
	}
	in.PriorAccrued = first.Amount
	again, err := computeCumulativeAccrual(in)
	if err != nil || again.Amount != 0 {
		t.Fatalf("chạy lại cùng ngày = %d, %v; muốn 0", again.Amount, err)
	}
}

// Lãi suất đổi giữa kỳ bằng RatePoint: tổng cả kỳ bằng lãi một lần chia theo mốc (449.315,07 → 449.000).
func TestCumulativeAccrualSplitsAtRatePoints(t *testing.T) {
	in := baseCumulative(100_000_000)
	in.PeriodStart = cd(2026, 7, 1)
	in.RatePoints = []interest.RatePoint{{Date: cd(2026, 7, 15), Rate: decimal.NewFromInt(5)}}
	_, total := simulatePeriod(t, in, 30, 0)
	if total != 449_000 {
		t.Fatalf("tổng = %d, muốn 449000", total)
	}
}

// Gửi thêm giữa kỳ: số dư đầu kỳ suy ngược từ số dư hiện tại; 15 ngày × 100 triệu + 16 ngày × 200 triệu = 772.602,74 → 773.000.
func TestCumulativeAccrualTopUpMidPeriod(t *testing.T) {
	in := baseCumulative(200_000_000) // số dư hiện tại đã gồm khoản gửi thêm
	in.TopUps = []topUp{{Date: cd(2026, 1, 16), Amount: decimal.NewFromInt(100_000_000)}}
	_, total := simulatePeriod(t, in, 31, 0)
	if total != 773_000 {
		t.Fatalf("tổng = %d, muốn 773000", total)
	}
	// ngày trước khi gửi thêm chỉ tính 100 triệu: 15 ngày = 246.575,34 → 247.000
	in.BusinessDate = cd(2026, 1, 15)
	res, err := computeCumulativeAccrual(in)
	if err != nil || res.Cumulative != 247_000 {
		t.Fatalf("lũy kế ngày 15 = %d, %v; muốn 247000", res.Cumulative, err)
	}
}

// Nhập gốc mở kỳ mới: kỳ sau bắt đầu ở mốc mới, "đã dự chi" chỉ tính các dòng của kỳ sau, không cộng chéo kỳ.
func TestCumulativeAccrualNewPeriodDoesNotCrossPeriods(t *testing.T) {
	first := baseCumulative(100_000_000)
	_, firstTotal := simulatePeriod(t, first, 10, 0)
	second := baseCumulative(101_000_000) // đã nhập gốc 1 triệu lãi
	second.PeriodStart = cd(2026, 1, 11)
	secondAmounts, secondTotal := simulatePeriod(t, second, 10, 0)
	// 101tr × 10 × 6 /36500 = 16.602,74 × ... (cả kỳ 2 = 166.027,40 → 166.000)
	if secondTotal != 166_000 {
		t.Fatalf("kỳ 2 tổng = %d (%v), muốn 166000", secondTotal, secondAmounts)
	}
	if firstTotal != 164_000 { // 100tr × 10 × 6 /36500 = 164.383,56 → 164.000
		t.Fatalf("kỳ 1 tổng = %d, muốn 164000", firstTotal)
	}
}

func TestCumulativeAccrualRoundNoZeroRoundsOnceToDong(t *testing.T) {
	in := baseCumulative(100_000_000)
	in.RoundNo = intp(0)
	in.BusinessDate = cd(2026, 1, 30)
	res, err := computeCumulativeAccrual(in)
	if err != nil || res.Amount != 493_151 { // 493.150,68 làm tròn đồng một lần
		t.Fatalf("ROUND_NO 0: %d, %v; muốn 493151", res.Amount, err)
	}
}

func TestCumulativeAccrualCM163Modes(t *testing.T) {
	for mode, want := range map[string]int64{
		interest.RoundHalfUp: 493_000, interest.RoundUp: 494_000, interest.RoundDown: 493_000,
	} {
		in := baseCumulative(100_000_000)
		in.RoundType = strp(mode)
		in.BusinessDate = cd(2026, 1, 30)
		res, err := computeCumulativeAccrual(in)
		if err != nil || res.Amount != want {
			t.Fatalf("%s: %d, %v; muốn %d", mode, res.Amount, err, want)
		}
	}
}

// Sản phẩm thiếu round_no/round_type (hoặc chỉ một trong hai) là lỗi cấu hình có mã, không đoán.
func TestCumulativeAccrualMissingRoundConfig(t *testing.T) {
	for name, mutate := range map[string]func(*cumulativeAccrualInput){
		"thiếu cả hai":     func(in *cumulativeAccrualInput) { in.RoundNo, in.RoundType = nil, nil },
		"thiếu round_no":   func(in *cumulativeAccrualInput) { in.RoundNo = nil },
		"thiếu round_type": func(in *cumulativeAccrualInput) { in.RoundType = nil },
	} {
		in := baseCumulative(100_000_000)
		mutate(&in)
		_, err := computeCumulativeAccrual(in)
		var typed *ardaerrors.Error
		if !errors.As(err, &typed) || typed.Code != CodeAccrualRoundConfigMissing {
			t.Fatalf("%s: lỗi = %v, muốn mã %s", name, err, CodeAccrualRoundConfigMissing)
		}
	}
}

func TestCumulativeAccrualBeforePeriodStartIsZero(t *testing.T) {
	in := baseCumulative(100_000_000)
	in.PeriodStart = cd(2026, 2, 1)
	in.BusinessDate = cd(2026, 1, 31)
	res, err := computeCumulativeAccrual(in)
	if err != nil || res.Amount != 0 || res.Cumulative != 0 {
		t.Fatalf("trước đầu kỳ: %+v, %v; muốn 0", res, err)
	}
}

// accrualWindow cắt tại mốc đầu kỳ: dòng mới không vắt qua hai kỳ.
func TestAccrualWindowCutsAtPeriodStart(t *testing.T) {
	// op nhập gốc hiệu lực 11/01, dòng POSTED gần nhất tới 08/01 (các ngày 09-10/01 dự chi 0, không có dòng)
	from, to, days, ok := accrualWindow("2026-01-01", "2026-01-08", "2026-01-12", "2026-01-11")
	if !ok || from != "2026-01-11" || to != "2026-01-13" || days != 2 {
		t.Fatalf("cửa sổ = (%s, %s, %d, %v), muốn (2026-01-11, 2026-01-13, 2, true)", from, to, days, ok)
	}
	// mốc kỳ trước ngày tiếp theo của dòng cuối thì dòng cuối thắng
	from, _, days, ok = accrualWindow("2026-01-01", "2026-01-20", "2026-01-22", "2026-01-11")
	if !ok || from != "2026-01-21" || days != 2 {
		t.Fatalf("cửa sổ = (%s, %d, %v), muốn (2026-01-21, 2, true)", from, days, ok)
	}
}
