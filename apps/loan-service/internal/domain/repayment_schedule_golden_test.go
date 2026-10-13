package domain

// Bảng case chuẩn — lịch trả nợ gốc đều (declining balance).
//
// Luồng: BuildEvenPrincipalPlans chia dư nợ thành N phần bằng nhau bằng
// ardamoney.AllocateEven (phần dư đơn vị nhỏ nhất dồn về các kỳ đầu, tổng gốc
// luôn bằng đúng dư nợ), lãi mỗi kỳ = dư nợ còn lại đầu kỳ × lãi suất
// năm/100/12 làm tròn nửa-lên về đơn vị tiền tệ. Kỳ i chạy từ
// start + (i-1) tháng đến start + i tháng theo time.AddDate (không kẹp cuối
// tháng — xem case "hiện trạng").
// Nguồn quy tắc: libs/go/arda-money (AllocateEven, MonthlyInterest),
// EPAS lnm_inf_repay_plan. Số tiền là minor unit.

import "testing"

type goldenPlanRow struct {
	termNo    int
	from, to  string
	principal int64
	interest  int64
}

func TestGoldenBuildEvenPrincipalPlans(t *testing.T) {
	cases := []struct {
		name      string
		agreement Agreement
		terms     int
		start     string
		want      []goldenPlanRow
	}{
		{
			// 1.000.000 / 3 = 333.333,33 → 333.333 ×3 = 999.999, dư 1 đồng dồn kỳ 1.
			// Lãi: 1.000.000×1% = 10.000; 666.666×1% = 6.666,66 → 6.667; 333.333×1% = 3.333,33 → 3.333.
			name:      "1 triệu VND, 3 kỳ, 12%/năm: phần dư 1 đồng dồn kỳ đầu, lãi giảm dần",
			agreement: Agreement{ContractCode: "HD-1", AgreementCode: "KU-1", CurrencyCode: "VND", OutstandingAmt: 1_000_000, InterestRate: 12},
			terms:     3,
			start:     "2026-01-15",
			want: []goldenPlanRow{
				{1, "2026-01-15", "2026-02-15", 333_334, 10_000},
				{2, "2026-02-15", "2026-03-15", 333_333, 6_667},
				{3, "2026-03-15", "2026-04-15", 333_333, 3_333},
			},
		},
		{
			// 100 / 12 = 8,33 → 8 ×12 = 96, dư 4 → 4 kỳ đầu nhận 9.
			// Lãi 1%/tháng trên dư nợ nhỏ: 100→1; 91→0,91→1; 82→1; 73→1; 64→1; 56→1;
			// 48→0,48→0 và các kỳ sau đều 0.
			name:      "100 đồng, 12 kỳ: phần dư 4 đồng rải 4 kỳ đầu, lãi lẻ dưới 0,5 làm tròn về 0",
			agreement: Agreement{AgreementCode: "KU-2", CurrencyCode: "VND", OutstandingAmt: 100, InterestRate: 12},
			terms:     12,
			start:     "2026-03-01",
			want: []goldenPlanRow{
				{1, "2026-03-01", "2026-04-01", 9, 1},
				{2, "2026-04-01", "2026-05-01", 9, 1},
				{3, "2026-05-01", "2026-06-01", 9, 1},
				{4, "2026-06-01", "2026-07-01", 9, 1},
				{5, "2026-07-01", "2026-08-01", 8, 1},
				{6, "2026-08-01", "2026-09-01", 8, 1},
				{7, "2026-09-01", "2026-10-01", 8, 0},
				{8, "2026-10-01", "2026-11-01", 8, 0},
				{9, "2026-11-01", "2026-12-01", 8, 0},
				{10, "2026-12-01", "2027-01-01", 8, 0},
				{11, "2027-01-01", "2027-02-01", 8, 0},
				{12, "2027-02-01", "2027-03-01", 8, 0},
			},
		},
		{
			name:      "1 kỳ duy nhất: toàn bộ gốc + 1 tháng lãi",
			agreement: Agreement{AgreementCode: "KU-3", CurrencyCode: "VND", OutstandingAmt: 250_000_000, InterestRate: 9.6},
			terms:     1,
			start:     "2026-06-30",
			want: []goldenPlanRow{
				// 250.000.000 × 9,6/1200 = 2.000.000
				{1, "2026-06-30", "2026-07-30", 250_000_000, 2_000_000},
			},
		},
		{
			// USD $1.000,00 / 3 = 333,33 ×3 = 999,99, dư 0,01 → kỳ 1 = 333,34.
			// Lãi 6%/năm = 0,5%/tháng: 1000→5,00; 666,66→3,3333→3,33; 333,33→1,66665→1,67.
			name:      "USD $1.000, 3 kỳ, 6%/năm: dư 1 cent dồn kỳ đầu, lãi làm tròn cent",
			agreement: Agreement{AgreementCode: "KU-4", CurrencyCode: "USD", OutstandingAmt: 100_000, InterestRate: 6},
			terms:     3,
			start:     "2026-01-01",
			want: []goldenPlanRow{
				{1, "2026-01-01", "2026-02-01", 33_334, 500},
				{2, "2026-02-01", "2026-03-01", 33_333, 333},
				{3, "2026-03-01", "2026-04-01", 33_333, 167},
			},
		},
		{
			// time.AddDate không kẹp cuối tháng: 31/01 + 1 tháng = "31/02" → 03/03/2026.
			// Deposit dùng AddMonthsClamped (kẹp về 28/02); loan chưa. Cần nghiệp vụ chốt.
			name:      "hiện trạng, cần xác nhận nghiệp vụ: bắt đầu 31/01 thì kỳ 1 kết thúc 03/03 (không kẹp cuối tháng)",
			agreement: Agreement{AgreementCode: "KU-5", CurrencyCode: "VND", OutstandingAmt: 2_000_000, InterestRate: 12},
			terms:     2,
			start:     "2026-01-31",
			want: []goldenPlanRow{
				{1, "2026-01-31", "2026-03-03", 1_000_000, 20_000},
				{2, "2026-03-03", "2026-03-31", 1_000_000, 10_000},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plans, err := BuildEvenPrincipalPlans(tc.agreement, tc.terms, tc.start)
			if err != nil {
				t.Fatalf("lỗi: %v", err)
			}
			if len(plans) != len(tc.want) {
				t.Fatalf("số kỳ = %d, muốn %d", len(plans), len(tc.want))
			}
			var totalPrincipal int64
			for i, want := range tc.want {
				got := plans[i]
				totalPrincipal += got.PlanPrincipalAmt
				if got.TermNo != want.termNo || got.FromDate != want.from || got.ToDate != want.to ||
					got.PlanPrincipalAmt != want.principal || got.PlanInterestAmt != want.interest {
					t.Fatalf("kỳ %d = {%d %s→%s gốc %d lãi %d}, muốn {%d %s→%s gốc %d lãi %d}",
						i+1, got.TermNo, got.FromDate, got.ToDate, got.PlanPrincipalAmt, got.PlanInterestAmt,
						want.termNo, want.from, want.to, want.principal, want.interest)
				}
				if got.PlanNo != 1 || got.AgreementCode != tc.agreement.AgreementCode || got.InterestRate != tc.agreement.InterestRate {
					t.Fatalf("kỳ %d sai định danh/lãi suất: %+v", i+1, got)
				}
			}
			if totalPrincipal != tc.agreement.OutstandingAmt {
				t.Fatalf("tổng gốc các kỳ = %d, muốn đúng dư nợ %d", totalPrincipal, tc.agreement.OutstandingAmt)
			}
		})
	}
}
