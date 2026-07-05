package worker

import "testing"

func TestUserTaskBrokerSignalComplete(t *testing.T) {
	broker := NewUserTaskBroker()
	ch := broker.Register(ParkedUserTask{JobKey: 123, JobType: "workflow.customer_checker_review", ProcessInstanceKey: 99})
	defer broker.Remove(123)

	if !broker.SignalComplete(123, map[string]any{"reviewDecision": "approve"}) {
		t.Fatal("expected signal to succeed")
	}
	got := <-ch
	if got["reviewDecision"] != "approve" {
		t.Fatalf("variables = %#v", got)
	}
	if broker.SignalComplete(999, map[string]any{}) {
		t.Fatal("unexpected signal for unknown job")
	}
}

func TestUserTaskBrokerFindParked(t *testing.T) {
	broker := NewUserTaskBroker()
	_ = broker.Register(ParkedUserTask{
		JobKey: 456, JobType: "workflow.customer_checker_review",
		ProcessInstanceKey: 2251799814526912, CaseID: "case-1",
	})
	defer broker.Remove(456)

	got := broker.FindParked(2251799814526912, "workflow.customer_checker_review")
	if got == nil || got.JobKey != 456 {
		t.Fatalf("parked = %#v", got)
	}
	if broker.FindParked(1, "workflow.customer_checker_review") != nil {
		t.Fatal("unexpected parked task for other process")
	}
}
