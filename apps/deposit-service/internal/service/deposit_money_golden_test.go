package service

// Bảng case chuẩn — luồng tiền deposit-service ở lớp service:
//
//   1. Cửa sổ dự chi (accrualWindow, DPM.305): từ ngày mở sổ hoặc ngày sau kỳ
//      POSTED gần nhất tới ngày nghiệp vụ. Phiên chạy dự chi TỚI HẾT ngày nghiệp
//      vụ nên cửa sổ nửa mở [from, to) có to = ngày nghiệp vụ + 1 (G2-DPM-NGAY-01
//      A / -02: nửa mở, dự chi liền kỳ không mất ngày); các case ghi "hiện trạng"
//      là hành vi cần xác nhận nghiệp vụ, không liên quan quy ước đếm ngày.
//   2. Số tiền dự chi một lần chạy = cửa sổ × interest.Calculate × Round(0),
//      đúng như RunDaily ghép lại (ghép lại ở đây vì RunDaily cần DB + finance).
//   3. Ngày đáo hạn = ngày mở + kỳ hạn tháng, kẹp cuối tháng (AddMonthsClamped).
//   4. Bút toán tất toán 3 leg DPM_SETTLEMENT_V3 (gốc, lãi dự chi, tiền chi) và
//      card trả lãi / nhập gốc (DPM_INTEREST_PAY, DPM_CAPITALIZE), card IBM —
//      khớp docs/accounting-rule-cards.md và seed
//      finance-service/migrations/20260911041000_dpm_interest_accounts.sql,
//      20260911030000_ibm_accounts_rules.sql.
// Số tiền là minor unit VND; mọi ngày là hằng số.

import (
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/interest"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/shopspring/decimal"
)

func TestGoldenAccrualWindow(t *testing.T) {
	cases := []struct {
		name                             string
		openDate, lastPosted, businessDate string
		wantFrom, wantTo                 string
		wantDays                         int
		wantOK                           bool
	}{
		{"nửa mở: mở sổ 01/01 chạy 01/01 → [01/01, 02/01) 1 ngày", "2026-01-01", "", "2026-01-01", "2026-01-01", "2026-01-02", 1, true},
		{"nửa mở: mở sổ 01/01 chạy 31/01 lần đầu → [01/01, 01/02) 31 ngày", "2026-01-01", "", "2026-01-31", "2026-01-01", "2026-02-01", 31, true},
		{"đã POSTED tới 31/01, chạy 01/02 → từ 01/02, 1 ngày", "2026-01-01", "2026-01-31", "2026-02-01", "2026-02-01", "2026-02-02", 1, true},
		{"qua năm: POSTED tới 31/12/2025, chạy 02/01/2026 → 2 ngày", "2025-12-01", "2025-12-31", "2026-01-02", "2026-01-01", "2026-01-03", 2, true},
		{"năm nhuận: POSTED tới 28/02/2028, chạy 01/03 → từ 29/02, 2 ngày", "2028-01-01", "2028-02-28", "2028-03-01", "2028-02-29", "2028-03-02", 2, true},
		{"năm thường: POSTED tới 28/02/2026, chạy 01/03 → 1 ngày", "2026-01-01", "2026-02-28", "2026-03-01", "2026-03-01", "2026-03-02", 1, true},
		{"đã POSTED đến đúng ngày chạy → không còn gì", "2026-01-01", "2026-01-31", "2026-01-31", "", "", 0, false},
		{"chạy trước ngày mở sổ → không tính", "2026-02-01", "", "2026-01-31", "", "", 0, false},
		// Không có mốc maturity_date trong cửa sổ: sau đáo hạn vẫn dự chi (BUG-10 mục 2).
		{"hiện trạng, cần xác nhận nghiệp vụ: sau ngày đáo hạn vẫn mở cửa sổ (không dừng ở maturity)", "2025-01-01", "2026-01-01", "2026-01-05", "2026-01-02", "2026-01-06", 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, days, ok := accrualWindow(tc.openDate, tc.lastPosted, tc.businessDate, "")
			if ok != tc.wantOK || from != tc.wantFrom || to != tc.wantTo || days != tc.wantDays {
				t.Fatalf("accrualWindow = (%s, %s, %d, %v), muốn (%s, %s, %d, %v)", from, to, days, ok, tc.wantFrom, tc.wantTo, tc.wantDays, tc.wantOK)
			}
		})
	}
}

// dailyAccrualMinor ghép đúng các bước RunDaily thực hiện cho một sổ.
func dailyAccrualMinor(t *testing.T, openDate, lastPosted, businessDate string, principal int64, rate float64, denominator int) (int, int64) {
	t.Helper()
	from, to, days, ok := accrualWindow(openDate, lastPosted, businessDate, "")
	if !ok {
		return 0, 0
	}
	fromDate, _ := time.Parse("2006-01-02", from)
	toDate, _ := time.Parse("2006-01-02", to)
	res := interest.Calculate(interest.Input{
		From: fromDate, To: toDate, Rate: decimal.NewFromFloat(rate), BaseDenominator: denominator, RoundNo: 0,
		Points: []interest.BalancePoint{{Date: fromDate, Balance: decimal.NewFromInt(principal)}},
	})
	return days, res.Rounded.Round(0).IntPart()
}

func TestGoldenDailyAccrualAmount(t *testing.T) {
	cases := []struct {
		name                             string
		openDate, lastPosted, businessDate string
		principal                        int64
		rate                             float64
		denominator                      int
		wantDays                         int
		wantMinor                        int64
	}{
		// 100tr × 1 × 6,5 /36500 = 17.808,22
		{"ngày đầu sau mở sổ, 100 triệu, 6,5% → 17.808", "2026-03-01", "", "2026-03-01", 100_000_000, 6.5, 365, 1, 17_808},
		{"ngày kế tiếp (đã POSTED hôm trước) cùng số tiền", "2026-03-01", "2026-03-01", "2026-03-02", 100_000_000, 6.5, 365, 1, 17_808},
		// EOD bỏ lỡ 3 ngày: 4 ngày × 17.808,22 = 71.232,88
		{"bù 4 ngày bị bỏ lỡ → 71.233 (không phải 4 × 17.808 = 71.232)", "2026-03-01", "2026-03-01", "2026-03-05", 100_000_000, 6.5, 365, 4, 71_233},
		// 50tr × 1 × 5,5 /36500 = 7.534,25
		{"50 triệu, 5,5%/năm → 7.534", "2026-03-01", "", "2026-03-01", 50_000_000, 5.5, 365, 1, 7_534},
		// 36tr × 1 × 10 /36000 = 10.000
		{"cơ sở 360: 36 triệu, 10% → đúng 10.000/ngày", "2026-03-01", "", "2026-03-01", 36_000_000, 10, 360, 1, 10_000},
		// 1.000 × 1 × 6 /36500 = 0,16 → 0 → RunDaily bỏ qua (amount <= 0)
		{"số dư nhỏ 1.000 đồng: lãi ngày 0,16 làm tròn 0 → không bút toán", "2026-03-01", "", "2026-03-01", 1_000, 6, 365, 1, 0},
		{"đã POSTED tới ngày chạy → không tính", "2026-03-01", "2026-03-05", "2026-03-05", 100_000_000, 6.5, 365, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			days, got := dailyAccrualMinor(t, tc.openDate, tc.lastPosted, tc.businessDate, tc.principal, tc.rate, tc.denominator)
			if days != tc.wantDays || got != tc.wantMinor {
				t.Fatalf("= %d ngày, %d đồng; muốn %d ngày, %d đồng", days, got, tc.wantDays, tc.wantMinor)
			}
		})
	}
}

func TestGoldenMaturityDate(t *testing.T) {
	cases := []struct {
		name     string
		openDate string
		months   int
		want     string
	}{
		{"kỳ hạn 1 tháng từ 15/01 → 15/02", "2026-01-15", 1, "2026-02-15"},
		{"31/01 + 1 tháng kẹp về 28/02 (năm thường)", "2026-01-31", 1, "2026-02-28"},
		{"31/01 + 1 tháng kẹp về 29/02 (năm nhuận)", "2028-01-31", 1, "2028-02-29"},
		{"30/01 + 1 tháng kẹp về 28/02", "2026-01-30", 1, "2026-02-28"},
		{"31/08 + 1 tháng kẹp về 30/09", "2026-08-31", 1, "2026-09-30"},
		{"31/01 + 3 tháng → 30/04", "2026-01-31", 3, "2026-04-30"},
		{"kỳ hạn 6 tháng từ 31/05 → 30/11", "2026-05-31", 6, "2026-11-30"},
		{"kỳ hạn 12 tháng từ 29/02/2028 → 28/02/2029", "2028-02-29", 12, "2029-02-28"},
		{"kỳ hạn 12 tháng từ 15/06 → 15/06 năm sau", "2026-06-15", 12, "2027-06-15"},
		{"kỳ hạn 13 tháng qua năm từ 30/11 → 30/12 năm sau", "2026-11-30", 13, "2027-12-30"},
		{"kỳ hạn 0 tháng giữ nguyên ngày", "2026-06-15", 0, "2026-06-15"},
		{"ngày không hợp lệ trả nguyên chuỗi", "15/06/2026", 1, "15/06/2026"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := addMonths(tc.openDate, tc.months); got != tc.want {
				t.Fatalf("addMonths(%s, %d) = %s, muốn %s", tc.openDate, tc.months, got, tc.want)
			}
		})
	}
}

var goldenDepositCards = map[string][]*financev1.PostingRule{
	"DPM_SETTLEMENT_V3": {
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "DPM_DEPOSIT_LIABILITY"},
		{LineNo: 2, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "DPM_INTEREST_PAYABLE"},
		{LineNo: 3, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "CASH_SETTLEMENT_ACCOUNT"},
	},
}

func TestGoldenSettlementV3Lines(t *testing.T) {
	analytics := func() *financev1.Analytics {
		return &financev1.Analytics{CustomerCode: "KH-1", Dimensions: map[string]string{"savings_code": "STK-1"}}
	}
	type line struct {
		direction      string
		amount         int64
		classification string
	}
	cases := []struct {
		name               string
		principal, accrued int64
		want               []line
	}{
		{"tất toán gốc 100 triệu + lãi dự chi 772.603: chi tiền 100.772.603", 100_000_000, 772_603, []line{
			{"DEBIT", 100_000_000, "DPM_DEPOSIT_LIABILITY"},
			{"DEBIT", 772_603, "DPM_INTEREST_PAYABLE"},
			{"CREDIT", 100_772_603, "CASH_SETTLEMENT_ACCOUNT"},
		}},
		{"lãi dự chi 0: leg lãi bị bỏ, còn 2 dòng gốc = tiền chi", 100_000_000, 0, []line{
			{"DEBIT", 100_000_000, "DPM_DEPOSIT_LIABILITY"},
			{"CREDIT", 100_000_000, "CASH_SETTLEMENT_ACCOUNT"},
		}},
		{"số nhỏ: gốc 1 + lãi 1 → chi 2", 1, 1, []line{
			{"DEBIT", 1, "DPM_DEPOSIT_LIABILITY"},
			{"DEBIT", 1, "DPM_INTEREST_PAYABLE"},
			{"CREDIT", 2, "CASH_SETTLEMENT_ACCOUNT"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legs := settlementV3Legs(tc.principal, tc.accrued, analytics)
			lines, err := financeclient.PostingLinesFromRules("DPM_SETTLEMENT_V3", goldenDepositCards["DPM_SETTLEMENT_V3"], legs, "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			if len(lines) != len(tc.want) {
				t.Fatalf("số dòng = %d, muốn %d", len(lines), len(tc.want))
			}
			var debit, credit int64
			for i, w := range tc.want {
				got := lines[i]
				if got.GetLineNo() != int32(i+1) || got.GetDirection() != w.direction || got.GetAmountMinor() != w.amount ||
					got.GetAnalytics().GetAccClassification() != w.classification {
					t.Fatalf("dòng %d = #%d %s %d %s, muốn %s %d %s", i+1, got.GetLineNo(), got.GetDirection(), got.GetAmountMinor(),
						got.GetAnalytics().GetAccClassification(), w.direction, w.amount, w.classification)
				}
				if got.GetAnalytics().GetDimensions()["savings_code"] != "STK-1" {
					t.Fatalf("dòng %d thiếu savings_code", i+1)
				}
				if got.GetDirection() == "DEBIT" {
					debit += got.GetAmountMinor()
				} else {
					credit += got.GetAmountMinor()
				}
			}
			if debit != credit {
				t.Fatalf("không cân: Nợ %d ≠ Có %d", debit, credit)
			}
		})
	}
}

func TestGoldenInterestOpCard(t *testing.T) {
	cases := []struct {
		opType                    string
		wantCard, wantDR, wantCR  string
	}{
		{"PAY", "DPM_INTEREST_PAY", "DPM_INTEREST_PAYABLE", "CASH_SETTLEMENT_ACCOUNT"},
		{"CAPITALIZE", "DPM_CAPITALIZE", "DPM_INTEREST_PAYABLE", "DPM_DEPOSIT_LIABILITY"},
	}
	for _, tc := range cases {
		t.Run("trả lãi kiểu "+tc.opType, func(t *testing.T) {
			card, dr, cr, _ := interestOpCard(tc.opType)
			if card != tc.wantCard || dr != tc.wantDR || cr != tc.wantCR {
				t.Fatalf("interestOpCard(%s) = %s DR %s / CR %s, muốn %s DR %s / CR %s", tc.opType, card, dr, cr, tc.wantCard, tc.wantDR, tc.wantCR)
			}
		})
	}
}

func TestGoldenIBMCards(t *testing.T) {
	cases := []struct {
		kind         string
		wantCard     string
		wantDR, wantCR string
	}{
		{IBMKindTopUp, "IBM_TOP_UP", "IBM_INTERBANK_DEPOSIT", "CASH_SETTLEMENT_ACCOUNT"},
		{IBMKindInterest, "IBM_INTEREST", "CASH_SETTLEMENT_ACCOUNT", "IBM_INTEREST_INCOME"},
		{IBMKindExpected, "IBM_EXPECTED", "IBM_INTEREST_RECEIVABLE", "IBM_INTEREST_INCOME"},
		{IBMKindWithdraw, "IBM_WITHDRAW", "CASH_SETTLEMENT_ACCOUNT", "IBM_INTERBANK_DEPOSIT"},
	}
	for _, tc := range cases {
		t.Run("IBM "+tc.kind, func(t *testing.T) {
			card := ibmCard(tc.kind)
			dr, cr := ibmLegs(card)
			if card != tc.wantCard || dr != tc.wantDR || cr != tc.wantCR {
				t.Fatalf("%s → %s DR %s / CR %s, muốn %s DR %s / CR %s", tc.kind, card, dr, cr, tc.wantCard, tc.wantDR, tc.wantCR)
			}
		})
	}
	if dr, cr := ibmLegs("IBM_PLACE"); dr != "IBM_INTERBANK_DEPOSIT" || cr != "CASH_SETTLEMENT_ACCOUNT" {
		t.Fatalf("IBM_PLACE = DR %s / CR %s", dr, cr)
	}
}
