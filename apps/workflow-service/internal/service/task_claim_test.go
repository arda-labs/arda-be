package service

import "testing"

func TestMatchesTaskClaimFilter(t *testing.T) {
	task := WorkflowTask{
		ProcessInstanceKey: 100,
		CaseID:             "case-1",
		ElementID:          "Activity_CheckerReview",
	}

	if !matchesTaskClaimFilter(task, TaskClaimFilter{ProcessInstanceKey: 100}) {
		t.Fatal("expected process instance match")
	}
	if matchesTaskClaimFilter(task, TaskClaimFilter{ProcessInstanceKey: 200}) {
		t.Fatal("expected process instance mismatch")
	}
	if !matchesTaskClaimFilter(task, TaskClaimFilter{CaseID: "case-1", ElementID: "Activity_CheckerReview"}) {
		t.Fatal("expected case and element match")
	}
	if matchesTaskClaimFilter(task, TaskClaimFilter{CaseID: "case-2"}) {
		t.Fatal("expected case mismatch")
	}
	if !matchesTaskClaimFilter(task, TaskClaimFilter{}) {
		t.Fatal("empty filter should accept any task")
	}
}
