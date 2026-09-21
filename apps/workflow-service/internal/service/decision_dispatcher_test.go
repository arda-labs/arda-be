package service

import (
	"context"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

type fakeDecisionAdapter struct {
	caseType string
	decision string
}

func (f fakeDecisionAdapter) Supports(d repository.TaskDecision) bool {
	return d.CaseType == f.caseType && d.Decision == f.decision
}

func (f fakeDecisionAdapter) Apply(context.Context, repository.TaskDecision) error { return nil }

func TestAdapterForSelectsFirstMatchingDomain(t *testing.T) {
	crm := fakeDecisionAdapter{caseType: "CUSTOMER_REGISTRATION", decision: "REQUEST_CHANGES"}
	loan := fakeDecisionAdapter{caseType: "LOAN_FORMATION_V2", decision: "REQUEST_CHANGES"}
	d := &DecisionDispatcher{
		adapters: []DecisionDomainAdapter{crm, loan},
		warned:   map[string]struct{}{},
	}

	got := d.adapterFor(repository.TaskDecision{CaseType: "LOAN_FORMATION_V2", Decision: "REQUEST_CHANGES"})
	if got != DecisionDomainAdapter(loan) {
		t.Fatalf("expected loan adapter, got %#v", got)
	}
	if d.adapterFor(repository.TaskDecision{CaseType: "DEPOSIT_OPEN_V2", Decision: "REQUEST_CHANGES"}) != nil {
		t.Fatal("expected nil adapter for an unowned decision")
	}
}

func TestWarnUnroutedDedupesAndRespectsGrace(t *testing.T) {
	d := &DecisionDispatcher{warned: map[string]struct{}{}}
	waiting := repository.TaskDecision{
		CaseType:   "DEPOSIT_OPEN_V2",
		Decision:   "REQUEST_CHANGES",
		RecordedAt: time.Now().Add(-2 * unroutedGracePeriod),
	}
	d.warnUnrouted(waiting)
	d.warnUnrouted(waiting)
	if len(d.warned) != 1 {
		t.Fatalf("expected one deduped warning, got %d", len(d.warned))
	}

	fresh := repository.TaskDecision{CaseType: "LOAN_FORMATION_V2", Decision: "SUBMIT", RecordedAt: time.Now()}
	d.warnUnrouted(fresh)
	if len(d.warned) != 1 {
		t.Fatal("a decision inside the grace period must not warn")
	}
}
