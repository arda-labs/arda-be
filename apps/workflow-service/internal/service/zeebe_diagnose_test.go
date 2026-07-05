package service

import "testing"

func TestJobTypesForDiagnoseSubmitted(t *testing.T) {
	got := jobTypesForDiagnose("CUSTOMER_REGISTRATION", "submitted")
	if len(got) != 2 || got[0] != "crm.mark_customer_submitted" {
		t.Fatalf("submitted types = %#v", got)
	}
}

func TestJobTypesForDiagnoseStep(t *testing.T) {
	got := jobTypesForDiagnose("CUSTOMER_REGISTRATION", "Activity_MakerRevise")
	if len(got) != 1 || got[0] != "workflow.customer_maker_revise" {
		t.Fatalf("maker revise types = %#v", got)
	}
}

func TestElementIDToJobType(t *testing.T) {
	if got := elementIDToJobType("Activity_CheckDuplicate"); got != "crm.check_customer_duplicate" {
		t.Fatalf("element map = %q", got)
	}
}
