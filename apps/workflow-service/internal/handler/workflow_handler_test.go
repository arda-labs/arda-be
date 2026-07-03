package handler

import "testing"

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

func TestTaskTypeForRequest(t *testing.T) {
	if got := taskTypeForRequest("FINANCE_TXN_MAKER", "workflow.finance_incoming_classify"); got != "workflow.finance_incoming_classify" {
		t.Fatalf("explicit task type = %q, want workflow.finance_incoming_classify", got)
	}
	if got := taskTypeForRequest("FINANCE_TXN_MAKER", "workflow.finance_incoming_approve"); got != "" {
		t.Fatalf("disallowed task type = %q, want empty", got)
	}
	if got := taskTypeForRequest("CUSTOMER_CHECKER", ""); got != "workflow.customer_checker_review" {
		t.Fatalf("role task type = %q, want workflow.customer_checker_review", got)
	}
	if got := taskTypeForRequest("UNKNOWN", ""); got != "" {
		t.Fatalf("unknown task type = %q, want empty", got)
	}
}
