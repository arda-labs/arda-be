package domain

// Bảng case chuẩn — hạn mức giải ngân theo hợp đồng (LNM.300.02 hai pha).
//
// Quy tắc (arda-refactor-plan.md BUG-2/BUG-4, T1.2): một mô hình duy nhất
// ContractExposure tiêu hạn mức hợp đồng:
//
//	đã dùng  = dư nợ (đã COMPLETE) + đang chuyển (đã REGISTER, chưa COMPLETE)
//	           + đang giữ chỗ (đề nghị đang xử lý, HELD)
//	còn lại  = số tiền vay − đã dùng
//
// Ba phần disjoint nên không bao giờ tính đôi; REGISTER chỉ được nhận khi
// 0 < số tiền ≤ còn lại. COMPLETE không tiêu hạn mức mà bị chặn bởi phần
// còn lại của REGISTER nguồn (CheckCompleteRemainder). Số tiền là minor unit.

import "testing"

func TestGoldenContractExposure(t *testing.T) {
	cases := []struct {
		name         string
		exposure     ContractExposure
		wantUsed     int64
		wantHeadroom int64
		allowed      []int64
		rejected     []int64
	}{
		{
			name:         "hợp đồng mới, chưa dùng gì: còn nguyên hạn mức",
			exposure:     ContractExposure{LoanAmountMinor: 1_000_000_000},
			wantUsed:     0,
			wantHeadroom: 1_000_000_000,
			allowed:      []int64{1, 500_000_000, 1_000_000_000},
			rejected:     []int64{0, -1, 1_000_000_001},
		},
		{
			name:         "đã giải ngân xong 400 triệu: còn 600 triệu",
			exposure:     ContractExposure{LoanAmountMinor: 1_000_000_000, OutstandingMinor: 400_000_000},
			wantUsed:     400_000_000,
			wantHeadroom: 600_000_000,
			allowed:      []int64{600_000_000},
			rejected:     []int64{600_000_001},
		},
		{
			name:         "đã REGISTER 400 triệu đang chuyển (chưa COMPLETE): vẫn tính là đã dùng, không tính đôi",
			exposure:     ContractExposure{LoanAmountMinor: 1_000_000_000, PendingMinor: 400_000_000},
			wantUsed:     400_000_000,
			wantHeadroom: 600_000_000,
			allowed:      []int64{600_000_000},
			rejected:     []int64{600_000_001},
		},
		{
			name:         "dư nợ + đang chuyển + giữ chỗ cộng dồn: 400 + 100 + 50 → còn 450 triệu",
			exposure:     ContractExposure{LoanAmountMinor: 1_000_000_000, OutstandingMinor: 400_000_000, PendingMinor: 100_000_000, ReservedMinor: 50_000_000},
			wantUsed:     550_000_000,
			wantHeadroom: 450_000_000,
			allowed:      []int64{450_000_000},
			rejected:     []int64{450_000_001},
		},
		{
			name:         "hai hồ sơ song song: hồ sơ A giữ chỗ 600 triệu nên hồ sơ B chỉ còn 400 (BUG-4)",
			exposure:     ContractExposure{LoanAmountMinor: 1_000_000_000, ReservedMinor: 600_000_000},
			wantUsed:     600_000_000,
			wantHeadroom: 400_000_000,
			allowed:      []int64{400_000_000},
			rejected:     []int64{400_000_001, 600_000_000},
		},
		{
			name:         "đã dùng hết hạn mức: không nhận thêm dù 1 đồng",
			exposure:     ContractExposure{LoanAmountMinor: 500_000_000, OutstandingMinor: 500_000_000},
			wantUsed:     500_000_000,
			wantHeadroom: 0,
			rejected:     []int64{1},
		},
		{
			name:         "vượt hạn mức (dữ liệu lệch): còn lại âm, từ chối mọi số tiền",
			exposure:     ContractExposure{LoanAmountMinor: 500_000_000, OutstandingMinor: 500_000_000, PendingMinor: 10},
			wantUsed:     500_000_010,
			wantHeadroom: -10,
			rejected:     []int64{1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.exposure.UsedMinor(); got != tc.wantUsed {
				t.Fatalf("UsedMinor = %d, muốn %d", got, tc.wantUsed)
			}
			if got := tc.exposure.HeadroomMinor(); got != tc.wantHeadroom {
				t.Fatalf("HeadroomMinor = %d, muốn %d", got, tc.wantHeadroom)
			}
			for _, amount := range tc.allowed {
				if !tc.exposure.Allows(amount) {
					t.Fatalf("Allows(%d) = false, muốn true", amount)
				}
			}
			for _, amount := range tc.rejected {
				if tc.exposure.Allows(amount) {
					t.Fatalf("Allows(%d) = true, muốn false", amount)
				}
			}
		})
	}
}

func TestGoldenCheckCompleteRemainder(t *testing.T) {
	cases := []struct {
		name                          string
		register, completed, complete int64
		wantErr                       bool
	}{
		{"COMPLETE lần đầu đúng bằng REGISTER", 500_000_000, 0, 500_000_000, false},
		{"COMPLETE từng phần: 200 rồi còn 300", 500_000_000, 200_000_000, 300_000_000, false},
		{"COMPLETE từng phần chưa hết: 200 + 100 < 500", 500_000_000, 200_000_000, 100_000_000, false},
		{"vượt phần còn lại 1 đồng", 500_000_000, 200_000_000, 300_000_001, true},
		{"REGISTER đã COMPLETE hết: không nhận thêm", 500_000_000, 500_000_000, 1, true},
		{"COMPLETE 0 đồng (dòng đóng hợp đồng) luôn hợp lệ", 500_000_000, 500_000_000, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckCompleteRemainder(tc.register, tc.completed, tc.complete)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckCompleteRemainder(%d, %d, %d) lỗi = %v, muốn lỗi %v", tc.register, tc.completed, tc.complete, err, tc.wantErr)
			}
		})
	}
}
