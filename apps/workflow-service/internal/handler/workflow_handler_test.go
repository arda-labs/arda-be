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
