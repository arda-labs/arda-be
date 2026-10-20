package service

import (
	"encoding/json"
	"testing"
)

// AP-PER-01: platform-service runStep đọc result.processed và result.failed
// của bước EOD để đánh FAILED khi lỗi một phần (eodStepOutcome). Đổi tên hai
// khoá JSON này sẽ làm lỗi từng khế ước lọt qua dưới dạng HTTP 200.
func TestEODJobResultsExposeStructuredStatus(t *testing.T) {
	cases := map[string]any{
		"accrual":   RunResult{Processed: 1, FailedDetail: []string{"LN-1: boom"}},
		"provision": ProvisionRunResult{Processed: 1, FailedAgmt: []string{"LN-1: boom"}},
	}
	for name, value := range cases {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var decoded struct {
			Processed *int      `json:"processed"`
			Failed    *[]string `json:"failed"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if decoded.Processed == nil || *decoded.Processed != 1 {
			t.Fatalf("%s: result.processed missing in %s", name, raw)
		}
		if decoded.Failed == nil || len(*decoded.Failed) != 1 {
			t.Fatalf("%s: result.failed missing in %s", name, raw)
		}
	}
}
