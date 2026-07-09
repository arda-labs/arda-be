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

func TestNextUserTaskAfterComplete(t *testing.T) {
	task, ok := NextUserTaskAfterComplete("CUSTOMER_REGISTRATION", "UT_MakerRevise", nil)
	if !ok || task.StepCode != "UT_CheckerReview" {
		t.Fatalf("maker revise should seed checker: ok=%v %+v", ok, task)
	}

	task, ok = NextUserTaskAfterComplete("CUSTOMER_REGISTRATION", "UT_CheckerReview", map[string]any{
		"reviewDecision": "REQUEST_CHANGES",
	})
	if !ok || task.StepCode != "UT_MakerRevise" {
		t.Fatalf("request changes should seed maker: ok=%v %+v", ok, task)
	}

	if _, ok := NextUserTaskAfterComplete("CUSTOMER_REGISTRATION", "UT_CheckerReview", map[string]any{
		"reviewDecision": "APPROVE",
	}); ok {
		t.Fatal("approve should not eager-seed a user task")
	}
}
