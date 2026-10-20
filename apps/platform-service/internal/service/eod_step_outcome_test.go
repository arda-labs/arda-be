package service

import (
	"strings"
	"testing"
)

func TestEODStepOutcomeReadsStructuredStatus(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		wantProcessed int
		wantFailed    int
		wantErr       bool
		wantDetail    string
	}{
		{name: "không có thân (204)", body: ""},
		{name: "chỉ khoảng trắng", body: " \n"},
		{name: "tính lãi thành công", body: `{"result":{"processed":3,"skipped":1},"success":true}`, wantProcessed: 3},
		{name: "mảng failed rỗng", body: `{"result":{"processed":3,"failed":[]},"success":true}`, wantProcessed: 3},
		{name: "một khế ước lỗi", body: `{"result":{"processed":2,"failed":["LN-002: boom"]},"success":true}`, wantProcessed: 2, wantFailed: 1, wantDetail: "LN-002: boom"},
		{name: "failed dạng số", body: `{"result":{"processed":5,"failed":2},"success":true}`, wantProcessed: 5, wantFailed: 2},
		{name: "success=false", body: `{"result":null,"success":false}`, wantFailed: 1},
		{name: "kết quả không phải object", body: `{"result":{"posted":4},"success":true}`},
		{name: "JSON hỏng", body: `{"result":`, wantErr: true},
		{name: "failed âm", body: `{"result":{"failed":-1}}`, wantErr: true},
		{name: "failed sai kiểu", body: `{"result":{"failed":"x"}}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eodStepOutcome([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("eodStepOutcome(%q) error = nil, want error", tc.body)
				}
				return
			}
			if err != nil {
				t.Fatalf("eodStepOutcome(%q): %v", tc.body, err)
			}
			if got.Processed != tc.wantProcessed || got.Failed != tc.wantFailed {
				t.Fatalf("outcome = %+v, want processed=%d failed=%d", got, tc.wantProcessed, tc.wantFailed)
			}
			if tc.wantDetail != "" && !strings.Contains(got.Detail, tc.wantDetail) {
				t.Fatalf("detail = %q, want it to contain %q", got.Detail, tc.wantDetail)
			}
		})
	}
}
