package domain

// Bảng case chuẩn — lãi tích lũy theo ngày (LNM.301.01, EOD accrual).
//
// Luồng: AccrualService.RunDaily đếm ngày từ lần tính lãi gần nhất (hoặc ngày
// giải ngân) đến ngày chạy bằng DaysBetween, rồi gọi AccruedInterestMinor:
//
//	lãi = dư nợ × (lãi suất năm × số ngày / 30) / 100 / 12
//
// tức là quy ước tháng 30 ngày, làm tròn nửa-lên (half away from zero) về
// đơn vị nhỏ nhất của tiền tệ (arda-money). Nguồn quy tắc:
// docs/accounting-rule-cards.md §LNM_ACCRUAL, libs/go/arda-money.
// Mọi số tiền là minor unit (VND không có phần thập phân).

import "testing"

func TestGoldenDaysBetween(t *testing.T) {
	cases := []struct {
		name     string
		from, to string
		want     int
	}{
		{"cùng ngày → 0 ngày", "2026-03-01", "2026-03-01", 0},
		{"đầu tháng đến cuối tháng 31 ngày → 30 ngày", "2026-01-01", "2026-01-31", 30},
		{"qua tháng 2 năm thường (2026) → 28 ngày", "2026-02-01", "2026-03-01", 28},
		{"qua tháng 2 năm nhuận (2028) → 29 ngày", "2028-02-01", "2028-03-01", 29},
		{"qua ngày 29/02 năm nhuận", "2028-02-28", "2028-03-01", 2},
		{"qua năm mới", "2025-12-31", "2026-01-02", 2},
		{"tròn một năm thường", "2026-01-01", "2027-01-01", 365},
		{"tròn một năm nhuận", "2028-01-01", "2029-01-01", 366},
		{"ngày đến trước ngày đi → số âm (service bỏ qua)", "2026-03-02", "2026-03-01", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DaysBetween(tc.from, tc.to)
			if err != nil {
				t.Fatalf("DaysBetween(%s, %s) lỗi: %v", tc.from, tc.to, err)
			}
			if got != tc.want {
				t.Fatalf("DaysBetween(%s, %s) = %d, muốn %d", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestGoldenAccruedInterestMinor(t *testing.T) {
	cases := []struct {
		name        string
		outstanding int64
		ratePercent float64
		days        int
		currency    string
		want        int64
	}{
		// 1 tỷ × 12%/năm × 30/30 ngày /100/12 = 10.000.000
		{"tròn 30 ngày, 1 tỷ, 12%/năm → đúng 1 tháng lãi", 1_000_000_000, 12, 30, "VND", 10_000_000},
		// 1 tỷ × (12 × 31/30 = 12,4)/100/12 = 10.333.333,33 → 10.333.333
		{"tháng 31 ngày tính 31/30 tháng → 10.333.333", 1_000_000_000, 12, 31, "VND", 10_333_333},
		// 1 tỷ × (12 × 29/30)/1200 = 9.666.666,67 → 9.666.667
		{"tháng 2 năm nhuận 29 ngày → 9.666.667", 1_000_000_000, 12, 29, "VND", 9_666_667},
		// 100 triệu × 8,5/30/1200 = 23.611,11 → 23.611
		{"1 ngày, 100 triệu, 8,5%/năm → 23.611", 100_000_000, 8.5, 1, "VND", 23_611},
		{"nửa tháng 15 ngày, 1 triệu, 12% → 5.000", 1_000_000, 12, 15, "VND", 5_000},
		// 500 triệu × 8,5 × 6/30 /1200 = 708.333,33 → 708.333
		{"6 ngày, 500 triệu, 8,5% → 708.333", 500_000_000, 8.5, 6, "VND", 708_333},
		{"0 ngày → 0", 1_000_000_000, 12, 0, "VND", 0},
		{"lãi suất 0% → 0", 1_000_000_000, 0, 30, "VND", 0},
		{"dư nợ 0 → 0", 0, 12, 30, "VND", 0},
		{"dư nợ âm được chặn về 0 (không sinh lãi âm)", -1_000_000, 12, 30, "VND", 0},
		// 50 × 12/1200 = 0,5 → làm tròn nửa-lên thành 1 đồng
		{"làm tròn nửa-lên: 0,5 đồng → 1 đồng", 50, 12, 30, "VND", 1},
		// 40 × 12/1200 = 0,4 → 0
		{"làm tròn: 0,4 đồng → 0 đồng", 40, 12, 30, "VND", 0},
		// 1.000.001 × 1% = 10.000,01 → 10.000
		{"phần lẻ dưới 0,5 bị bỏ", 1_000_001, 12, 30, "VND", 10_000},
		// USD: 1.234,56 × 6%/12 = 6,1728 → 6,17 USD = 617 cent
		{"USD làm tròn về cent: $1.234,56 × 6%/12 → 617 cent", 123_456, 6, 30, "USD", 617},
		{"tiền tệ rỗng mặc định VND", 1_000_000, 12, 15, "", 5_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AccruedInterestMinor(tc.outstanding, tc.ratePercent, tc.days, tc.currency)
			if err != nil {
				t.Fatalf("lỗi: %v", err)
			}
			if got != tc.want {
				t.Fatalf("AccruedInterestMinor(%d, %v, %d, %q) = %d, muốn %d",
					tc.outstanding, tc.ratePercent, tc.days, tc.currency, got, tc.want)
			}
		})
	}
}
