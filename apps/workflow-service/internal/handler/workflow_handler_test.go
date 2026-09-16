package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"google.golang.org/grpc/metadata"
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

func TestPermissionFilteredWorkItems(t *testing.T) {
	newItems := func() []repository.WorkItem {
		return []repository.WorkItem{
			{CaseID: "case-1", CandidateRole: "CUSTOMER_CHECKER"},
			{CaseID: "case-2", CandidateRole: "OTHER_ROLE"},
		}
	}

	r := httptest.NewRequest("GET", "/api/workflow/work-items/export", nil)
	r.Header.Set("X-User-Roles", "CUSTOMER_CHECKER")
	items, err := (&WorkflowHandler{}).permissionFilteredWorkItems(context.Background(), r, newItems())
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(items) != 1 || items[0].CaseID != "case-1" {
		t.Fatalf("default direction visible = %+v, want only case-1", items)
	}

	// direction=ALL keeps rows (search must stay usable) but still annotates
	// each item with its resolved permissions.
	r = httptest.NewRequest("GET", "/api/workflow/work-items/export?direction=ALL", nil)
	r.Header.Set("X-User-Roles", "CUSTOMER_CHECKER")
	items, err = (&WorkflowHandler{}).permissionFilteredWorkItems(context.Background(), r, newItems())
	if err != nil {
		t.Fatalf("filter ALL: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("direction=ALL visible = %d, want 2", len(items))
	}
	if items[1].CanView {
		t.Fatal("unrelated candidate role must not be viewable")
	}
}

func TestRequiredWorkflowTargetTenant(t *testing.T) {
	// Missing verified tenant scope is rejected, even without a query param.
	if _, err := requiredWorkflowTargetTenant(workflowTenantRequest("GET", "/api/workflow/role-memberships", "")); err == nil {
		t.Fatal("missing verified tenant was accepted")
	}
	// A query param restating the verified tenant is honored.
	r := workflowTenantRequest("GET", "/api/workflow/role-memberships?tenant_id=tenant-1", "tenant-1")
	got, err := requiredWorkflowTargetTenant(r)
	if err != nil || got != "tenant-1" {
		t.Fatalf("requiredWorkflowTargetTenant() = %q, %v", got, err)
	}
	// No query param: the verified tenant is used.
	r = workflowTenantRequest("GET", "/api/workflow/role-memberships", "tenant-1")
	got, err = requiredWorkflowTargetTenant(r)
	if err != nil || got != "tenant-1" {
		t.Fatalf("requiredWorkflowTargetTenant() = %q, %v", got, err)
	}
	// Incoming metadata (gRPC-originated contexts) is also accepted.
	r = httptest.NewRequest("GET", "/api/workflow/role-memberships", nil)
	r = r.WithContext(metadata.NewIncomingContext(r.Context(), metadata.Pairs(ardametadata.TenantID, "tenant-1")))
	got, err = requiredWorkflowTargetTenant(r)
	if err != nil || got != "tenant-1" {
		t.Fatalf("requiredWorkflowTargetTenant(incoming) = %q, %v", got, err)
	}
	// A different tenant in the query param is rejected for tenant admins.
	r = workflowTenantRequest("GET", "/api/workflow/role-memberships?tenant_id=tenant-2", "tenant-1")
	if _, err := requiredWorkflowTargetTenant(r); !errors.Is(err, errWorkflowTenantOutsideScope) {
		t.Fatalf("cross-tenant target was accepted: %v", err)
	}
	// Superadmin may explicitly target another tenant.
	r = workflowTenantRequest("GET", "/api/workflow/role-memberships?tenant_id=tenant-2", "tenant-1")
	r.Header.Set("X-Global-Admin", "true")
	got, err = requiredWorkflowTargetTenant(r)
	if err != nil || got != "tenant-2" {
		t.Fatalf("superadmin requiredWorkflowTargetTenant() = %q, %v", got, err)
	}
	// Superadmin without a query param still stays inside the verified tenant.
	r = workflowTenantRequest("GET", "/api/workflow/role-memberships", "tenant-1")
	r.Header.Set("X-Global-Admin", "true")
	got, err = requiredWorkflowTargetTenant(r)
	if err != nil || got != "tenant-1" {
		t.Fatalf("superadmin requiredWorkflowTargetTenant() = %q, %v", got, err)
	}
}

func TestRoleMembershipsRejectsTenantOutsideVerifiedScope(t *testing.T) {
	h := &WorkflowHandler{}

	w := httptest.NewRecorder()
	h.RoleMemberships(w, workflowTenantRequest("GET", "/api/workflow/role-memberships?tenant_id=tenant-2", "tenant-1"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant status = %d, want 403", w.Code)
	}

	w = httptest.NewRecorder()
	h.RoleMemberships(w, workflowTenantRequest("GET", "/api/workflow/role-memberships", ""))
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing verified tenant status = %d, want 403", w.Code)
	}
}

func TestAuthenticatedActorForCase(t *testing.T) {
	if _, err := authenticatedActorForCase(httptest.NewRequest("POST", "/api/workflow/cases", nil), "someone"); !errors.Is(err, errCaseActorRequired) {
		t.Fatalf("missing verified actor err = %v", err)
	}

	r := httptest.NewRequest("POST", "/api/workflow/cases", nil)
	r.Header.Set("X-User-Id", "maker-1")
	got, err := authenticatedActorForCase(r, "")
	if err != nil || got != "maker-1" {
		t.Fatalf("authenticatedActorForCase(empty body) = %q, %v", got, err)
	}
	got, err = authenticatedActorForCase(r, "maker-1")
	if err != nil || got != "maker-1" {
		t.Fatalf("authenticatedActorForCase(matching body) = %q, %v", got, err)
	}
	if _, err := authenticatedActorForCase(r, "checker-9"); !errors.Is(err, errCaseActorMismatch) {
		t.Fatalf("forged body actor was accepted: %v", err)
	}

	r.Header.Set("X-Global-Admin", "true")
	got, err = authenticatedActorForCase(r, "maker-2")
	if err != nil || got != "maker-2" {
		t.Fatalf("superadmin impersonation = %q, %v", got, err)
	}
}

func workflowTenantRequest(method, target, tenantID string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	if tenantID == "" {
		return r
	}
	ctx := ardametadata.AppendToOutgoing(r.Context(), ardametadata.Context{
		TenantID:    tenantID,
		AuthChecked: "true",
	})
	return r.WithContext(ctx)
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
