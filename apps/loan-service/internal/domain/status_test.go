package domain

import (
	"errors"
	"testing"
)

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name    string
		machine StateMachine
		from    Status
		to      Status
		reason  string
		wantErr bool
	}{
		{name: "draft submit", machine: WorkflowMachine, from: StatusDraft, to: StatusPendingApproval},
		{name: "submit approve", machine: WorkflowMachine, from: StatusPendingApproval, to: StatusApproved},
		{name: "submit reject needs reason", machine: WorkflowMachine, from: StatusPendingApproval, to: StatusRejected, wantErr: true},
		{name: "submit reject with reason", machine: WorkflowMachine, from: StatusPendingApproval, to: StatusRejected, reason: "missing documents"},
		{name: "rejected revise", machine: WorkflowMachine, from: StatusRejected, to: StatusDraft},
		{name: "approved post", machine: WorkflowMachine, from: StatusApproved, to: StatusPosted},
		{name: "posted reverse needs reason", machine: WorkflowMachine, from: StatusPosted, to: StatusReversed, wantErr: true},
		{name: "posted reverse with reason", machine: WorkflowMachine, from: StatusPosted, to: StatusReversed, reason: "cancel approved transaction"},
		{name: "cannot skip approval", machine: WorkflowMachine, from: StatusDraft, to: StatusPosted, wantErr: true},
		{name: "contract approve then disburse", machine: ContractMachine, from: StatusApproved, to: StatusDisbursed},
		{name: "agreement closes", machine: AgreementMachine, from: StatusActive, to: StatusClosed},
		{name: "schedule superseded", machine: PlanLifecycleMachine, from: StatusActive, to: StatusSuperseded},
		{name: "schedule starts paying", machine: PlanPaymentMachine, from: StatusPlanned, to: StatusPartiallyPaid},
		{name: "schedule paid", machine: PlanPaymentMachine, from: StatusPartiallyPaid, to: StatusPaid},
		{name: "payment does not supersede", machine: PlanPaymentMachine, from: StatusPlanned, to: StatusSuperseded, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanTransition(tt.machine, tt.from, tt.to, tt.reason)
			if tt.wantErr && !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("CanTransition() error = %v, want ErrInvalidTransition", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CanTransition() error = %v", err)
			}
		})
	}
}

func TestEveryStatusHasAPath(t *testing.T) {
	for machine, statuses := range map[StateMachine][]Status{
		WorkflowMachine:      {StatusDraft, StatusPendingApproval, StatusApproved, StatusRejected, StatusPosted, StatusCancelled, StatusReversed},
		ContractMachine:      {StatusDraft, StatusPendingApproval, StatusApproved, StatusRejected, StatusCancelled, StatusDisbursed, StatusClosed},
		AgreementMachine:     {StatusActive, StatusClosed},
		PlanLifecycleMachine: {StatusActive, StatusSuperseded},
		PlanPaymentMachine:   {StatusPlanned, StatusPartiallyPaid, StatusPaid},
	} {
		for _, status := range statuses {
			if !HasTransitionPath(machine, status) {
				t.Errorf("status %s in machine %s has no transition path", status, machine)
			}
		}
	}
}
