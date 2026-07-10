package repository

import (
	"strings"
	"testing"
)

func TestWorkItemSeedStatusRequiresJobKeyForReady(t *testing.T) {
	if got := workItemSeedStatus(WorkItemSeed{}); got != TaskStatusRouting {
		t.Fatalf("seed without job key = %s, want %s", got, TaskStatusRouting)
	}
	jobKey := int64(42)
	if got := workItemSeedStatus(WorkItemSeed{JobKey: &jobKey}); got != TaskStatusReady {
		t.Fatalf("seed with job key = %s, want %s", got, TaskStatusReady)
	}
}

func TestIncomingWorkItemsIncludeRoutingAndReadyOrClaimedTasks(t *testing.T) {
	incoming := strings.Join(incomingWorkItemWhere(), " ")
	for _, fragment := range []string{
		"wt.status = 'ROUTING'",
		"wt.status = 'READY'",
		"wt.status = 'CLAIMED'",
		"wt.job_key IS NOT NULL",
	} {
		if !strings.Contains(incoming, fragment) {
			t.Fatalf("incoming list must include %q: %s", fragment, incoming)
		}
	}
}

func TestDedupeWorkItemsByCase(t *testing.T) {
	items := []WorkItem{
		{ID: "a", CaseID: "case-1"},
		{ID: "b", CaseID: "case-1"},
		{ID: "c", CaseID: "case-2"},
	}
	got := dedupeWorkItemsByCase(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("unexpected dedupe order: %+v", got)
	}
}

func TestIsMakerTrackCaseType(t *testing.T) {
	if !isMakerTrackCaseType("CUSTOMER_REGISTRATION") {
		t.Fatal("registration should be maker-tracked")
	}
	if isMakerTrackCaseType("FINANCE_INCOMING_TRANSACTION") {
		t.Fatal("finance incoming should not be maker-tracked")
	}
}

func TestWorkItemDecorateMakerOutgoing(t *testing.T) {
	item := WorkItem{
		CaseType:  "CUSTOMER_REGISTRATION",
		CreatedBy: "maker-1",
		Status:    TaskStatusCompleted,
	}
	item.decorate("maker-1", "OUTGOING")
	if item.Direction != "OUTGOING" {
		t.Fatalf("expected OUTGOING direction, got %s", item.Direction)
	}
	if !item.CanOpen || item.CanClaim {
		t.Fatalf("maker outgoing should be view-only: canOpen=%v canClaim=%v", item.CanOpen, item.CanClaim)
	}
}

func TestWorkItemDecorateMakerRoutingIsViewOnly(t *testing.T) {
	item := WorkItem{
		CaseType:  "CUSTOMER_REGISTRATION",
		CreatedBy: "maker-1",
		Status:    TaskStatusRouting,
	}
	item.decorate("maker-1", "OUTGOING")
	if !item.CanOpen || item.CanClaim {
		t.Fatalf("maker routing should be tracking only: canOpen=%v canClaim=%v", item.CanOpen, item.CanClaim)
	}
}
