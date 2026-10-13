package domain

// Bảng case chuẩn — chiến lược bút toán và ma trận chuyển nhóm nợ
// (T3.4, LNM.305 phân loại nợ): mỗi lần chuyển bảo toàn đúng số minor unit,
// chỉ các cặp nhóm đã cấu hình mới được chuyển, không được chuyển quá số dư
// nguồn, không đổi map đầu vào. Số tiền minor unit VND.

import (
	"errors"
	"reflect"
	"testing"
)

func TestGoldenParsePostingStrategy(t *testing.T) {
	for _, code := range []string{"SIMPLE", "BAL_TYPE_SPLIT", "DEBT_GROUP_RECLASS"} {
		if s, err := ParsePostingStrategy(code); err != nil || string(s) != code {
			t.Fatalf("%s: %v", code, err)
		}
	}
	for _, code := range []string{"", "simple", "RECLASS"} {
		if _, err := ParsePostingStrategy(code); !errors.Is(err, ErrUnknownPostingStrategy) {
			t.Fatalf("%q phải là ErrUnknownPostingStrategy, nhận %v", code, err)
		}
	}
}

func TestGoldenDebtGroupReclass(t *testing.T) {
	matrix, err := NewDebtGroupMatrix([]DebtGroupTransition{
		{From: "GROUP_1", To: "GROUP_2"}, {From: "GROUP_2", To: "GROUP_3"}, {From: "GROUP_3", To: "GROUP_4"},
		{From: "GROUP_4", To: "GROUP_5"}, {From: "GROUP_2", To: "GROUP_1"}, {From: "GROUP_3", To: "GROUP_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := map[string]int64{"GROUP_1": 700_000_000, "GROUP_2": 300_000_000, "GROUP_3": 0}
	cases := []struct {
		name      string
		transfers []DebtGroupTransfer
		want      map[string]int64
		wantErr   bool
	}{
		{"xuống nhóm 2 200 triệu: 1 giảm, 2 tăng, tổng 1 tỷ giữ nguyên", []DebtGroupTransfer{{"GROUP_1", "GROUP_2", 200_000_000}},
			map[string]int64{"GROUP_1": 500_000_000, "GROUP_2": 500_000_000, "GROUP_3": 0}, false},
		{"chuyển toàn bộ 300 triệu nhóm 2 sang nhóm 3", []DebtGroupTransfer{{"GROUP_2", "GROUP_3", 300_000_000}},
			map[string]int64{"GROUP_1": 700_000_000, "GROUP_2": 0, "GROUP_3": 300_000_000}, false},
		{"chuỗi 1→2→3 trong một lần áp", []DebtGroupTransfer{{"GROUP_1", "GROUP_2", 100_000_000}, {"GROUP_2", "GROUP_3", 400_000_000}},
			map[string]int64{"GROUP_1": 600_000_000, "GROUP_2": 0, "GROUP_3": 400_000_000}, false},
		{"lên nhóm tốt hơn 2→1 (đã cấu hình)", []DebtGroupTransfer{{"GROUP_2", "GROUP_1", 300_000_000}},
			map[string]int64{"GROUP_1": 1_000_000_000, "GROUP_2": 0, "GROUP_3": 0}, false},
		{"vào nhóm chưa có trong bảng số dư (GROUP_4) vẫn tạo được", []DebtGroupTransfer{{"GROUP_2", "GROUP_3", 300_000_000}, {"GROUP_3", "GROUP_4", 300_000_000}},
			map[string]int64{"GROUP_1": 700_000_000, "GROUP_2": 0, "GROUP_3": 0, "GROUP_4": 300_000_000}, false},
		{"không có chuyển: giữ nguyên", nil, start, false},
		{"vượt số dư nguồn 1 đồng → lỗi", []DebtGroupTransfer{{"GROUP_2", "GROUP_3", 300_000_001}}, nil, true},
		{"cặp chưa cấu hình 1→3 → lỗi", []DebtGroupTransfer{{"GROUP_1", "GROUP_3", 1}}, nil, true},
		{"số tiền 0 → lỗi", []DebtGroupTransfer{{"GROUP_1", "GROUP_2", 0}}, nil, true},
		{"chuyển vào chính nó → lỗi", []DebtGroupTransfer{{"GROUP_1", "GROUP_1", 1}}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matrix.Apply(start, tc.transfers)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidDebtGroupTransfer) {
					t.Fatalf("muốn ErrInvalidDebtGroupTransfer, nhận %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("= %v, muốn %v", got, tc.want)
			}
			var before, after int64
			for _, v := range start {
				before += v
			}
			for _, v := range got {
				after += v
			}
			if before != after {
				t.Fatalf("tổng dư nợ đổi %d → %d", before, after)
			}
			if !reflect.DeepEqual(start, map[string]int64{"GROUP_1": 700_000_000, "GROUP_2": 300_000_000, "GROUP_3": 0}) {
				t.Fatalf("Apply sửa map đầu vào: %v", start)
			}
		})
	}
	if _, err := matrix.Apply(map[string]int64{"GROUP_1": -1}, nil); !errors.Is(err, ErrInvalidDebtGroupTransfer) {
		t.Fatalf("số dư âm phải bị từ chối: %v", err)
	}
	for _, bad := range [][]DebtGroupTransition{{{From: "A", To: "A"}}, {{From: "", To: "B"}}, {{From: "A", To: "B"}, {From: "A", To: "B"}}} {
		if _, err := NewDebtGroupMatrix(bad); !errors.Is(err, ErrInvalidDebtGroupTransition) {
			t.Fatalf("cấu hình %v phải bị từ chối: %v", bad, err)
		}
	}
}
