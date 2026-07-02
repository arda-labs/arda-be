package service

import (
	"regexp"
	"testing"
)

func TestFinanceFoundationRules(t *testing.T) {
	entryID := newEntryID()
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(entryID) {
		t.Fatalf("entry id is not a v4 uuid: %q", entryID)
	}

	svc := NewApprovalService(nil, nil, nil)
	if got := svc.DetermineLevels("TRANSFER", 500_000_000); got != 3 {
		t.Fatalf("TRANSFER 500m levels = %d, want 3", got)
	}
	if got := svc.DetermineLevels("INCOMING_TRANSFER", 1_000); got != 2 {
		t.Fatalf("unknown incoming levels = %d, want default 2", got)
	}
}
