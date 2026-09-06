package service

import (
	"context"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

type fakeAssignmentSource struct {
	rule        *repository.WorkflowAssignmentRule
	members     map[string][]string
	delegations map[string][]string
}

func (f *fakeAssignmentSource) FindAssignmentRule(ctx context.Context, caseType, stepCode string) (*repository.WorkflowAssignmentRule, error) {
	return f.rule, nil
}

func (f *fakeAssignmentSource) ListActiveMembershipUsers(ctx context.Context, tenantID, roleCode string) ([]string, error) {
	return f.members[roleCode], nil
}

func (f *fakeAssignmentSource) ListActiveDelegationTargets(ctx context.Context, tenantID, roleCode string, members []string) ([]string, error) {
	var out []string
	for _, member := range members {
		out = append(out, f.delegations[member]...)
	}
	return out, nil
}

func TestAssignmentResolverNoRuleKeepsBPMNCandidates(t *testing.T) {
	resolver := NewAssignmentResolver(&fakeAssignmentSource{})
	result := resolver.Resolve(context.Background(), AssignmentRequest{CaseType: "CRM_X", StepCode: "step-1", TenantID: "t1"})
	if result.Resolved {
		t.Fatal("expected no rule to mean unresolved")
	}
	if len(result.CandidateUsers) != 0 {
		t.Fatalf("expected no candidates, got %v", result.CandidateUsers)
	}
}

func TestAssignmentResolverResolvesPoolAndDelegations(t *testing.T) {
	resolver := NewAssignmentResolver(&fakeAssignmentSource{
		rule: &repository.WorkflowAssignmentRule{
			CaseType: "LOAN_V2", StepCode: "UT_CheckerReview", RoleCode: "LOAN_CHECKER",
			AssignmentMode: "CANDIDATE_POOL", RequireSeparationOfDuties: true,
		},
		members: map[string][]string{"LOAN_CHECKER": {"maker-a", "checker-b", "checker-c"}},
		delegations: map[string][]string{"checker-b": {"checker-d"}},
	})
	result := resolver.Resolve(context.Background(), AssignmentRequest{
		CaseType: "LOAN_V2", StepCode: "UT_CheckerReview", TenantID: "t1", CreatedBy: "maker-a",
	})
	if !result.Resolved || result.RoleCode != "LOAN_CHECKER" {
		t.Fatalf("unexpected result: %+v", result)
	}
	for _, user := range result.CandidateUsers {
		if user == "maker-a" {
			t.Fatal("separation of duties must exclude the case maker from the pool")
		}
	}
	foundDelegation := false
	for _, user := range result.CandidateUsers {
		if user == "checker-d" {
			foundDelegation = true
		}
	}
	if !foundDelegation {
		t.Fatalf("expected delegated substitute in pool, got %v", result.CandidateUsers)
	}
}

func TestAssignmentResolverFallsBackToSecondaryRole(t *testing.T) {
	resolver := NewAssignmentResolver(&fakeAssignmentSource{
		rule: &repository.WorkflowAssignmentRule{
			CaseType: "LOAN_V2", StepCode: "UT_Approve", RoleCode: "BRANCH_MANAGER",
			AssignmentMode: "DIRECT", FallbackRoleCode: "DEPUTY_MANAGER",
		},
		members: map[string][]string{"DEPUTY_MANAGER": {"deputy-1"}},
	})
	result := resolver.Resolve(context.Background(), AssignmentRequest{CaseType: "LOAN_V2", StepCode: "UT_Approve", TenantID: "t1"})
	if !result.Resolved || result.RoleCode != "DEPUTY_MANAGER" {
		t.Fatalf("expected fallback role resolution, got %+v", result)
	}
	if len(result.CandidateUsers) != 1 || result.CandidateUsers[0] != "deputy-1" {
		t.Fatalf("expected fallback members, got %v", result.CandidateUsers)
	}
}
