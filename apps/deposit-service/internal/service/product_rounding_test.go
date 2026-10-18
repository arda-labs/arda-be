package service

import (
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/deposit-service/internal/interest"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// Cặp round_no / round_type của sản phẩm: cả hai hoặc không có cái nào, round_no >= 0, kiểu thuộc CM163.
func TestValidateProductRounding(t *testing.T) {
	tests := []struct {
		name    string
		no      *int
		kind    *string
		wantErr bool
	}{
		{"không cấu hình (giữ nguyên khi sửa)", nil, nil, false},
		{"1.000 HALF_UP", intp(1000), strp(interest.RoundHalfUp), false},
		{"1.000 CEILING", intp(1000), strp(interest.RoundUp), false},
		{"1.000 FLOOR", intp(1000), strp(interest.RoundDown), false},
		{"0 giữ lãi thập phân, làm tròn đồng một lần", intp(0), strp(interest.RoundHalfUp), false},
		{"chỉ round_no", intp(1000), nil, true},
		{"chỉ round_type", nil, strp(interest.RoundHalfUp), true},
		{"round_no âm", intp(-5), strp(interest.RoundHalfUp), true},
		{"kiểu lạ", intp(1000), strp("CM163.999"), true},
		{"kiểu tên thay vì mã", intp(1000), strp("HALF_UP"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProductRounding(tc.no, tc.kind)
			if (err != nil) != tc.wantErr {
				t.Fatalf("lỗi = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				var typed *ardaerrors.Error
				if !errors.As(err, &typed) || typed.Code != ardaerrors.CodeInvalidInput {
					t.Fatalf("mã lỗi = %v, muốn %s", err, ardaerrors.CodeInvalidInput)
				}
			}
		})
	}
}
