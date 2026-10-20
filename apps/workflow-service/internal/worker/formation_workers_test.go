package worker

import (
	"strings"
	"testing"
)

// TestFormationContractIDRequiresCaseContract: worker execute/cancel chỉ đổi
// trạng thái hợp đồng của chính hồ sơ (primaryObjectId); contractId bị ghi đè
// thành hợp đồng khác thì job bị từ chối thay vì duyệt nhầm hợp đồng.
func TestFormationContractIDRequiresCaseContract(t *testing.T) {
	id, err := formationContractID(map[string]any{"primaryObjectId": "contract-1", "contractId": "contract-1"})
	if err != nil || id != "contract-1" {
		t.Fatalf("matching ids = %q, %v; want contract-1", id, err)
	}
	id, err = formationContractID(map[string]any{"primaryObjectId": "contract-1"})
	if err != nil || id != "contract-1" {
		t.Fatalf("primaryObjectId only = %q, %v; want contract-1", id, err)
	}
	if _, err := formationContractID(map[string]any{"primaryObjectId": "contract-1", "contractId": "contract-2"}); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched contractId error = %v, want mismatch", err)
	}
	if _, err := formationContractID(map[string]any{"contractId": "contract-2"}); err == nil ||
		!strings.Contains(err.Error(), "primaryObjectId") {
		t.Fatalf("missing primaryObjectId error = %v, want missing primaryObjectId", err)
	}
}
