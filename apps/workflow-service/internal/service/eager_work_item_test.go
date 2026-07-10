package service

import "testing"

func TestInitialUserTaskForCaseType(t *testing.T) {
	task, ok := InitialUserTaskForCaseType("CUSTOMER_REGISTRATION")
	if !ok || task.StepCode != "UT_MakerRevise" || task.CandidateRole != "CUSTOMER_MAKER" {
		t.Fatalf("unexpected initial task: ok=%v %+v", ok, task)
	}
	if _, ok := InitialUserTaskForCaseType("FINANCE_INCOMING_TRANSACTION"); ok {
		t.Fatal("finance should not eager-seed yet")
	}
}

func TestNextUserTaskAfterMakerCompletionSeedsCheckerReview(t *testing.T) {
	task, ok := NextUserTaskAfterComplete("CUSTOMER_REGISTRATION", "UT_MakerRevise", nil)
	if !ok {
		t.Fatal("maker completion must seed the checker task")
	}
	if task.StepCode != "UT_CheckerReview" || task.CandidateRole != "CUSTOMER_CHECKER" {
		t.Fatalf("unexpected checker task: %+v", task)
	}
}

func TestNextUserTaskAfterRequestChangesSeedsMakerRevision(t *testing.T) {
	task, ok := NextUserTaskAfterComplete("CUSTOMER_REGISTRATION", "UT_CheckerReview", map[string]any{
		"reviewDecision": "REQUEST_CHANGES",
	})
	if !ok {
		t.Fatal("request changes must seed the maker task")
	}
	if task.StepCode != "UT_MakerRevise" || task.CandidateRole != "CUSTOMER_MAKER" {
		t.Fatalf("unexpected maker task: %+v", task)
	}
}

func TestNextUserTaskAcceptsApprovalResultForRequestChanges(t *testing.T) {
	task, ok := NextUserTaskAfterComplete("CUSTOMER_REGISTRATION", "UT_CheckerReview", map[string]any{
		"approvalResult": "REQUEST_CHANGES",
	})
	if !ok || task.StepCode != "UT_MakerRevise" {
		t.Fatalf("approvalResult request changes must seed maker task: ok=%v %+v", ok, task)
	}
}
