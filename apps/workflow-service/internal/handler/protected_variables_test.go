package handler

import (
	"reflect"
	"testing"
)

// TestTaskCompletionVariablesDropsRoutingKeys: người giữ task không ghi đè được
// biến định tuyến phê duyệt (số tiền, hạn mức PGD/GD, nguồn vốn, hợp đồng) hay
// khóa dành riêng; dữ liệu bước và quyết định vẫn được gửi sang engine.
func TestTaskCompletionVariablesDropsRoutingKeys(t *testing.T) {
	variables, dropped := taskCompletionVariables(map[string]any{
		"amount":           float64(1),
		"pgdLimit":         float64(1e15),
		"gdLimit":          float64(1e15),
		"fundSource":       "BRANCH",
		"contractId":       "contract-other",
		"approvalLevel":    "PGD",
		"tenantId":         "tenant-attacker",
		"mcCheckerRole":    "ADMIN",
		"reviewDecision":   "APPROVE",
		"approvalNumber":   "QD-01",
		"appraisalOfficer": "officer-1",
	})
	wantDropped := []string{"amount", "approvalLevel", "contractId", "fundSource", "gdLimit", "mcCheckerRole", "pgdLimit", "tenantId"}
	if !reflect.DeepEqual(dropped, wantDropped) {
		t.Fatalf("dropped = %v, want %v", dropped, wantDropped)
	}
	want := map[string]any{
		"reviewDecision":   "APPROVE",
		"decision":         "APPROVE",
		"approvalNumber":   "QD-01",
		"appraisalOfficer": "officer-1",
	}
	if !reflect.DeepEqual(variables, want) {
		t.Fatalf("variables = %v, want %v", variables, want)
	}
	if got, dropped := taskCompletionVariables(nil); got != nil || len(dropped) != 0 {
		t.Fatalf("taskCompletionVariables(nil) = %v, %v; want nil, none", got, dropped)
	}
}
