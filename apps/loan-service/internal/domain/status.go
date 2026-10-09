package domain

import (
	"errors"
	"fmt"
	"strings"
)

type Status string

type StateMachine string

const (
	WorkflowMachine      StateMachine = "workflow"
	ContractMachine      StateMachine = "contract"
	AgreementMachine     StateMachine = "agreement"
	PlanLifecycleMachine StateMachine = "repay_plan_lifecycle"
	PlanPaymentMachine   StateMachine = "repay_plan_payment"
)

const (
	StatusDraft           Status = "DRAFT"
	StatusPendingApproval Status = "PENDING_APPROVAL"
	StatusSubmitFailed    Status = "SUBMIT_FAILED"
	StatusApproved        Status = "APPROVED"
	StatusRejected        Status = "REJECTED"
	StatusPosted          Status = "POSTED"
	StatusCancelled       Status = "CANCELLED"
	StatusReversed        Status = "REVERSED"
	StatusDisbursed       Status = "DISBURSED"
	StatusClosed          Status = "CLOSED"
	StatusActive          Status = "ACTIVE"
	StatusSuperseded      Status = "SUPERSEDED"
	StatusPlanned         Status = "PLANNED"
	StatusPartiallyPaid   Status = "PARTIALLY_PAID"
	StatusPaid            Status = "PAID"
)

var ErrInvalidTransition = errors.New("invalid status transition")
var ErrAdjustmentNotPending = errors.New("lnm: adjustment is not pending")

type transition struct {
	from Status
	to   Status
}

var allowedTransitions = map[StateMachine][]transition{
	WorkflowMachine: {
		{StatusDraft, StatusPendingApproval},
		{StatusDraft, StatusCancelled},
		{StatusPendingApproval, StatusSubmitFailed},
		{StatusSubmitFailed, StatusPendingApproval},
		{StatusPendingApproval, StatusApproved},
		{StatusPendingApproval, StatusRejected},
		{StatusPendingApproval, StatusCancelled},
		{StatusRejected, StatusDraft},
		{StatusApproved, StatusPosted},
		{StatusPosted, StatusReversed},
	},
	ContractMachine: {
		{StatusDraft, StatusPendingApproval},
		{StatusDraft, StatusCancelled},
		{StatusPendingApproval, StatusApproved},
		{StatusPendingApproval, StatusRejected},
		{StatusPendingApproval, StatusCancelled},
		{StatusRejected, StatusDraft},
		{StatusApproved, StatusDisbursed},
		{StatusDisbursed, StatusClosed},
	},
	AgreementMachine: {
		{StatusActive, StatusClosed},
	},
	PlanLifecycleMachine: {
		{StatusActive, StatusSuperseded},
	},
	PlanPaymentMachine: {
		{StatusPlanned, StatusPartiallyPaid},
		{StatusPlanned, StatusPaid},
		{StatusPartiallyPaid, StatusPaid},
	},
}

// CanTransition checks a named aggregate state machine. Rejection,
// cancellation, and reversal require an audit reason.
func CanTransition(machine StateMachine, from, to Status, reason string) error {
	if machine == "" || from == "" || to == "" {
		return fmt.Errorf("%w: machine, from and to are required", ErrInvalidTransition)
	}
	for _, edge := range allowedTransitions[machine] {
		if edge.from == from && edge.to == to {
			if (to == StatusRejected || to == StatusCancelled || to == StatusReversed) && strings.TrimSpace(reason) == "" {
				return fmt.Errorf("%w: %s requires a reason", ErrInvalidTransition, to)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %s cannot move from %s to %s", ErrInvalidTransition, machine, from, to)
}

// HasTransitionPath reports whether status participates in at least one edge
// in the named state machine. Terminal states have an incoming path.
func HasTransitionPath(machine StateMachine, status Status) bool {
	for _, edge := range allowedTransitions[machine] {
		if edge.from == status || edge.to == status {
			return true
		}
	}
	return false
}

// AdjustmentDecisionStatus maps a workflow decision to its terminal target.
func AdjustmentDecisionStatus(decision string) (Status, error) {
	switch strings.ToUpper(decision) {
	case "APPROVE":
		return AdjustmentActive, nil
	case "REJECT":
		return AdjustmentRejected, nil
	case "CANCEL":
		return AdjustmentCancelled, nil
	default:
		return "", fmt.Errorf("unknown decision %q", decision)
	}
}

// CheckAdjustmentReplay accepts only a replay that already reached the same
// target status; other misses indicate a conflicting or incomplete decision.
func CheckAdjustmentReplay(currentStatus, targetStatus string) error {
	if currentStatus == targetStatus {
		return nil
	}
	return fmt.Errorf("%w (status=%s)", ErrAdjustmentNotPending, currentStatus)
}
