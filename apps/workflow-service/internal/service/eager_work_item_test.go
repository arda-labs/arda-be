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
