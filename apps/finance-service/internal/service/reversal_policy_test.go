package service

import "testing"

// Danh mục loại chứng từ được hủy (AP-REV-01): chứng từ domain luôn bị từ
// chối; FIN_* thủ công theo mặc định; cờ allow_cancel ghi đè trong FIN_*.
func TestReversalAllowed(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name     string
		domain   string
		docType  string
		override *bool
		want     bool
	}{
		{"thủ công một bên", "fin", "FIN_SINGLE_ENTRY", nil, true},
		{"thủ công hai bên", "fin", "FIN_DOUBLE_ENTRY", nil, true},
		{"ngoại bảng", "fin", "FIN_OFF_BALANCE", nil, true},
		{"trích lập quỹ", "fin", "FIN_FUND_APPROP", nil, true},
		{"sử dụng quỹ", "fin", "FIN_FUND_USE", nil, true},
		{"kết chuyển không mặc định", "fin", "FIN_CLOSING", nil, false},
		{"bút toán đảo không đảo lại", "fin", "FIN_TXN_CANCEL", nil, false},
		{"thu nợ khoản vay", "lnm", "LNM_COLLECTION", nil, false},
		{"giải ngân", "lnm", "LNM_DISBURSEMENT", nil, false},
		{"dự thu", "lnm", "LNM_ACCRUAL_DAILY", nil, false},
		{"domain khác mượn mã FIN_", "lnm", "FIN_DOUBLE_ENTRY", nil, false},
		{"domain khác dù bật cờ", "dpm", "FIN_DOUBLE_ENTRY", &yes, false},
		{"tắt hủy loại mặc định", "fin", "FIN_DOUBLE_ENTRY", &no, false},
		{"bật hủy kết chuyển", "fin", "FIN_CLOSING", &yes, true},
		{"không phải FIN_ dù bật cờ", "fin", "CASH_RECEIPT", &yes, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reversalAllowed(tc.domain, tc.docType, tc.override); got != tc.want {
				t.Fatalf("reversalAllowed(%s, %s) = %v, muốn %v", tc.domain, tc.docType, got, tc.want)
			}
		})
	}
}
