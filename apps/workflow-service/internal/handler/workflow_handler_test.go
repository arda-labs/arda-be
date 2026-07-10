package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

func TestApplyWorkItemPermissionsRoutingIsNotActionable(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/workflow/work-items", nil)
	r.Header.Set("X-User-Roles", "CUSTOMER_CHECKER")
	items := []repository.WorkItem{{
		Status:        repository.TaskStatusRouting,
		CandidateRole: "CUSTOMER_CHECKER",
	}}

	if err := (&WorkflowHandler{}).applyWorkItemPermissions(context.Background(), r, items); err != nil {
		t.Fatalf("apply permissions: %v", err)
	}
	if items[0].CanClaim || items[0].CanOpen {
		t.Fatalf("routing task must stay non-actionable: canClaim=%v canOpen=%v", items[0].CanClaim, items[0].CanOpen)
	}
	if items[0].ClaimBlockedReason == "" {
		t.Fatal("routing task must explain why it cannot be opened")
	}
}

func TestCasePath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantID     string
		wantAction string
	}{
		{name: "detail", path: "/api/workflow/cases/case-1", wantID: "case-1"},
		{name: "timeline", path: "/api/workflow/cases/case-1/timeline", wantID: "case-1", wantAction: "timeline"},
		{name: "too deep", path: "/api/workflow/cases/case-1/timeline/extra"},
		{name: "wrong prefix", path: "/api/workflow/case-types"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, action := casePath(tt.path)
			if id != tt.wantID || action != tt.wantAction {
				t.Fatalf("casePath() = (%q, %q), want (%q, %q)", id, action, tt.wantID, tt.wantAction)
			}
		})
	}
}

func TestCaseTypePath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantID     string
		wantAction string
	}{
		{name: "catalog", path: "/api/workflow/case-types/CUSTOMER_REGISTRATION", wantID: "CUSTOMER_REGISTRATION"},
		{name: "process config", path: "/api/workflow/case-types/CUSTOMER_REGISTRATION/process-config", wantID: "CUSTOMER_REGISTRATION", wantAction: "process-config"},
		{name: "too deep", path: "/api/workflow/case-types/CUSTOMER_REGISTRATION/process-config/extra"},
		{name: "wrong prefix", path: "/api/workflow/cases/case-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, action := caseTypePath(tt.path)
			if id != tt.wantID || action != tt.wantAction {
				t.Fatalf("caseTypePath() = (%q, %q), want (%q, %q)", id, action, tt.wantID, tt.wantAction)
			}
		})
	}
}

func TestTaskPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantKey    int64
		wantAction string
	}{
		{name: "complete", path: "/api/workflow/tasks/123/complete", wantKey: 123, wantAction: "complete"},
		{name: "bad key", path: "/api/workflow/tasks/nope/complete"},
		{name: "too shallow", path: "/api/workflow/tasks/123"},
		{name: "claim route", path: "/api/workflow/tasks/claim"},
		{name: "too deep", path: "/api/workflow/tasks/123/complete/extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, action := taskPath(tt.path)
			if key != tt.wantKey || action != tt.wantAction {
				t.Fatalf("taskPath() = (%d, %q), want (%d, %q)", key, action, tt.wantKey, tt.wantAction)
			}
		})
	}
}

func TestWorkItemPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantID     string
		wantAction string
	}{
		{name: "claim", path: "/api/workflow/work-items/task-1/claim", wantID: "task-1", wantAction: "claim"},
		{name: "detail", path: "/api/workflow/work-items/task-1", wantID: "task-1"},
		{name: "collection", path: "/api/workflow/work-items"},
		{name: "too deep", path: "/api/workflow/work-items/task-1/claim/extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, action := workItemPath(tt.path)
			if id != tt.wantID || action != tt.wantAction {
				t.Fatalf("workItemPath() = (%q, %q), want (%q, %q)", id, action, tt.wantID, tt.wantAction)
			}
		})
	}
}

func TestTaskTypeForRequest(t *testing.T) {
	if got := taskTypeForRequest("CUSTOMER_CHECKER", ""); got != "" {
		t.Fatalf("legacy task type = %q, want empty", got)
	}
	if got := taskTypeForRequest("FINANCE_TXN_MAKER", "workflow.finance_incoming_classify"); got != "" {
		t.Fatalf("legacy explicit task type = %q, want empty", got)
	}
}

func TestWorkItemSeedFromCaseSkipsLegacy(t *testing.T) {
	bpmnV2 := "crm-customer-registration-v2"
	_, ok := workItemSeedFromCase(repository.BusinessCase{
		CaseType:      "CUSTOMER_REGISTRATION",
		BpmnProcessID: &bpmnV2,
		CurrentStep:   "UT_MakerRevise",
		Status:        repository.CaseStatusInReview,
	})
	if ok {
		t.Fatal("expected v2 case to skip legacy work item seed")
	}

	legacyProcess := "legacy-process"
	_, ok = workItemSeedFromCase(repository.BusinessCase{
		CaseType:      "CUSTOMER_REGISTRATION",
		BpmnProcessID: &legacyProcess,
		CurrentStep:   "Activity_MakerRevise",
		Status:        repository.CaseStatusInReview,
	})
	if ok {
		t.Fatal("expected v1 case to skip legacy work item seed")
	}
}
