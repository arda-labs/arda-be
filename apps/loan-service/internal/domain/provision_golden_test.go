package domain

// Bảng case chuẩn — dự phòng rủi ro tín dụng (LNM.306 cụ thể, LNM.307 chung,
// batch EOD theo nhóm nợ).
//
// Quy tắc (docs/accounting-rule-cards.md §LNM_PROVISION, TT 02/2023, CM130):
//   - tỷ lệ theo nhóm nợ: GROUP_1 0% · GROUP_2 5% · GROUP_3 20% · GROUP_4 50% ·
//     GROUP_5 100%;
//   - phải trích = cơ sở × tỷ lệ / 100, làm tròn nửa-lên về đơn vị nhỏ nhất;
//   - delta = phải trích − đã trích: dương là trích thêm (card line 1-2), âm là
//     hoàn nhập (card line 3-4).
// Cơ sở của 306 = max(dư nợ − khấu trừ tài sản bảo đảm, 0) được tính ở
// service (cần DB) nên bảng này chỉ khóa phép nhân/làm tròn/delta.
// Số tiền là minor unit.

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestGoldenRequiredProvisionPercent(t *testing.T) {
	cases := []struct {
		name string
		base int64
		rate float64
		want int64
	}{
		{"200 tỷ × 0,75% dự phòng chung = 1,5 tỷ", 200_000_000_000, 0.75, 1_500_000_000},
		// 333.333.333 × 0,75% = 2.499.999,9975 → 2.500.000
		{"làm tròn nửa-lên phần lẻ 0,9975", 333_333_333, 0.75, 2_500_000},
		{"1 tỷ nhóm 2 (5%) = 50 triệu", 1_000_000_000, 5, 50_000_000},
		{"1 tỷ nhóm 3 (20%) = 200 triệu", 1_000_000_000, 20, 200_000_000},
		{"1 tỷ nhóm 4 (50%) = 500 triệu", 1_000_000_000, 50, 500_000_000},
		{"nhóm 5 (100%) trích bằng toàn bộ dư nợ", 123_456_789, 100, 123_456_789},
		// 10 × 5% = 0,5 → 1; 9 × 5% = 0,45 → 0
		{"0,5 đồng làm tròn lên 1 đồng", 10, 5, 1},
		{"0,45 đồng làm tròn về 0", 9, 5, 0},
		// 1.000.001 × 5% = 50.000,05 → 50.000; 1.000.010 × 5% = 50.000,5 → 50.001
		{"phần lẻ 0,05 bị bỏ", 1_000_001, 5, 50_000},
		{"phần lẻ 0,5 làm tròn lên", 1_000_010, 5, 50_001},
		{"dư nợ 0 → 0", 0, 20, 0},
		{"dư nợ âm → 0", -1_000_000, 20, 0},
		{"tỷ lệ 0 (nhóm 1) → 0", 1_000_000_000, 0, 0},
		{"tỷ lệ âm bị coi như 0", 1_000_000_000, -5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RequiredGeneralProvision(tc.base, tc.rate); got != tc.want {
				t.Fatalf("RequiredGeneralProvision(%d, %v) = %d, muốn %d", tc.base, tc.rate, got, tc.want)
			}
			// 306 dùng cùng công thức trên cơ sở sau khấu trừ.
			if got := RequiredSpecificProvision(tc.base, tc.rate); got != tc.want {
				t.Fatalf("RequiredSpecificProvision(%d, %v) = %d, muốn %d", tc.base, tc.rate, got, tc.want)
			}
		})
	}
}

func TestGoldenDebtGroupProvisionRate(t *testing.T) {
	cases := []struct {
		group string
		want  string
		ok    bool
	}{
		{"GROUP_1", "0", true},
		{"GROUP_2", "5", true},
		{"GROUP_3", "20", true},
		{"GROUP_4", "50", true},
		{"GROUP_5", "100", true},
		{"GROUP_6", "", false},
		{"", "", false},
		{"group_3", "", false},
	}
	for _, tc := range cases {
		t.Run("nhóm nợ "+tc.group, func(t *testing.T) {
			rate, ok := DebtGroupProvisionRate(tc.group)
			if ok != tc.ok {
				t.Fatalf("ok = %v, muốn %v", ok, tc.ok)
			}
			if ok && rate.String() != tc.want {
				t.Fatalf("tỷ lệ = %s%%, muốn %s%%", rate, tc.want)
			}
		})
	}
}

func TestGoldenRequiredProvisionMinorByDebtGroup(t *testing.T) {
	cases := []struct {
		name        string
		outstanding int64
		group       string
		currency    string
		want        int64
	}{
		{"500 triệu nhóm 3 → 100 triệu (batch EOD)", 500_000_000, "GROUP_3", "VND", 100_000_000},
		{"400 triệu nhóm 3 → 80 triệu (giảm dư nợ kéo theo hoàn nhập)", 400_000_000, "GROUP_3", "VND", 80_000_000},
		{"nhóm 1 không trích", 500_000_000, "GROUP_1", "VND", 0},
		{"nhóm 5 trích 100%", 500_000_000, "GROUP_5", "VND", 500_000_000},
		// 333.333.333 × 5% = 16.666.666,65 → 16.666.667
		{"nhóm 2 làm tròn nửa-lên", 333_333_333, "GROUP_2", "VND", 16_666_667},
		{"tiền tệ rỗng mặc định VND", 1_000, "GROUP_2", "", 50},
		// $1.234,56 × 5% = 61,728 USD; Round(0) làm tròn về ĐÔ LA (62,00) trước khi
		// đổi ra cent → 6.200 cent, không phải 6.173. Hiện mọi dự phòng đều là VND.
		{"hiện trạng, cần xác nhận nghiệp vụ: USD làm tròn về đơn vị lớn (62,00 USD) thay vì cent", 123_456, "GROUP_2", "USD", 6_200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rate, ok := DebtGroupProvisionRate(tc.group)
			if !ok {
				t.Fatalf("nhóm %s không có tỷ lệ", tc.group)
			}
			got, err := RequiredProvisionMinor(tc.outstanding, rate, tc.currency)
			if err != nil {
				t.Fatalf("lỗi: %v", err)
			}
			if got != tc.want {
				t.Fatalf("RequiredProvisionMinor(%d, %s%%, %q) = %d, muốn %d", tc.outstanding, rate, tc.currency, got, tc.want)
			}
		})
	}
}

func TestGoldenProvisionDeltas(t *testing.T) {
	cases := []struct {
		name                 string
		required, accumulated int64
		wantSigned           int64 // ProvisionDelta (batch EOD, âm = hoàn nhập)
		wantAlloc            int64 // GeneralProvisionDelta / SpecificProvisionDelta: trích thêm
		wantReverse          int64 // hoàn nhập
	}{
		{"chưa trích lần nào: trích đủ số phải có", 100_000_000, 0, 100_000_000, 100_000_000, 0},
		{"dư nợ tăng: trích thêm phần chênh", 120_000_000, 100_000_000, 20_000_000, 20_000_000, 0},
		{"dư nợ giảm / chuyển nhóm tốt hơn: hoàn nhập phần chênh", 80_000_000, 100_000_000, -20_000_000, 0, 20_000_000},
		{"không đổi: không có bút toán", 100_000_000, 100_000_000, 0, 0, 0},
		{"tất toán (phải trích 0): hoàn nhập toàn bộ", 0, 100_000_000, -100_000_000, 0, 100_000_000},
		{"cả hai bằng 0", 0, 0, 0, 0, 0},
		{"chênh 1 đồng trích thêm", 1_000_001, 1_000_000, 1, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProvisionDelta(tc.required, tc.accumulated); got != tc.wantSigned {
				t.Fatalf("ProvisionDelta = %d, muốn %d", got, tc.wantSigned)
			}
			alloc, reverse := GeneralProvisionDelta(tc.required, tc.accumulated)
			if alloc != tc.wantAlloc || reverse != tc.wantReverse {
				t.Fatalf("GeneralProvisionDelta = (%d, %d), muốn (%d, %d)", alloc, reverse, tc.wantAlloc, tc.wantReverse)
			}
			add, reversal := SpecificProvisionDelta(tc.required, tc.accumulated)
			if add != tc.wantAlloc || reversal != tc.wantReverse {
				t.Fatalf("SpecificProvisionDelta = (%d, %d), muốn (%d, %d)", add, reversal, tc.wantAlloc, tc.wantReverse)
			}
		})
	}
}

// Dự phòng không bao giờ trả về giá trị thập phân: mọi hàm đều làm tròn về
// số nguyên minor unit trước khi quay lại int64.
func TestGoldenProvisionRoundingIsHalfUpNotBankers(t *testing.T) {
	// 2,5 và 3,5 đều làm tròn lên (half away from zero), không phải banker's rounding.
	if got := RequiredGeneralProvision(50, 5); got != 3 { // 50 × 5% = 2,5
		t.Fatalf("2,5 → %d, muốn 3", got)
	}
	if got := RequiredGeneralProvision(70, 5); got != 4 { // 70 × 5% = 3,5
		t.Fatalf("3,5 → %d, muốn 4", got)
	}
	got, err := RequiredProvisionMinor(50, decimal.NewFromInt(5), "VND")
	if err != nil || got != 3 {
		t.Fatalf("batch 2,5 → %d (%v), muốn 3", got, err)
	}
}
