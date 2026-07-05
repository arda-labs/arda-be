package service

import "testing"

func TestInboxJobTypesForRolePrefersPrimary(t *testing.T) {
	got := inboxJobTypesForRole("CUSTOMER_MAKER")
	if len(got) == 0 || got[0] != "workflow.customer_maker_revise" {
		t.Fatalf("job types = %#v, want maker revise first", got)
	}
}

func TestInboxJobTypesForRoleIncludesCheckerFallback(t *testing.T) {
	got := inboxJobTypesForRole("CUSTOMER_MAKER")
	found := false
	for _, jobType := range got {
		if jobType == "workflow.customer_checker_review" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("job types = %#v, want checker review fallback", got)
	}
}
