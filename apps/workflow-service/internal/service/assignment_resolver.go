package service

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

// AssignmentRuleSource abstracts the persisted assignment configuration so
// resolution stays unit-testable without a database.
type AssignmentRuleSource interface {
	FindAssignmentRule(ctx context.Context, caseType, stepCode string) (*repository.WorkflowAssignmentRule, error)
	ListActiveMembershipUsers(ctx context.Context, tenantID, roleCode string) ([]string, error)
	ListActiveDelegationTargets(ctx context.Context, tenantID, roleCode string, members []string) ([]string, error)
}

// AssignmentRequest identifies the task step to resolve candidates for.
type AssignmentRequest struct {
	CaseType  string
	StepCode  string
	TenantID  string
	CreatedBy string
}

// AssignmentResult is the resolved candidate configuration for one step.
type AssignmentResult struct {
	Resolved       bool
	RoleCode       string
	CandidateUsers []string
	AssignmentMode string
}

// AssignmentResolver turns persisted assignment rules + role memberships into
// a concrete candidate pool for a task step. Closes workflow-service Known
// Gap #2: rules and memberships were persisted but never consulted.
type AssignmentResolver struct {
	source AssignmentRuleSource
}

func NewAssignmentResolver(source AssignmentRuleSource) *AssignmentResolver {
	if source == nil {
		return nil
	}
	return &AssignmentResolver{source: source}
}

func (r *AssignmentResolver) Enabled() bool {
	return r != nil && r.source != nil
}

// Resolve falls back to an empty result (not resolved) whenever configuration
// is missing — the BPMN-declared candidate groups remain authoritative then.
func (r *AssignmentResolver) Resolve(ctx context.Context, req AssignmentRequest) AssignmentResult {
	if !r.Enabled() {
		return AssignmentResult{}
	}
	rule, err := r.source.FindAssignmentRule(ctx, req.CaseType, req.StepCode)
	if err != nil {
		slog.Warn("assignment resolver: rule lookup failed", "caseType", req.CaseType, "stepCode", req.StepCode, "err", err)
		return AssignmentResult{}
	}
	if rule == nil {
		return AssignmentResult{}
	}

	roleCode := rule.RoleCode
	users, err := r.source.ListActiveMembershipUsers(ctx, req.TenantID, roleCode)
	if err != nil {
		slog.Warn("assignment resolver: membership lookup failed", "roleCode", roleCode, "err", err)
		users = nil
	}
	if len(users) == 0 && rule.FallbackRoleCode != "" && rule.FallbackRoleCode != roleCode {
		fallbackUsers, err := r.source.ListActiveMembershipUsers(ctx, req.TenantID, rule.FallbackRoleCode)
		if err != nil {
			slog.Warn("assignment resolver: fallback membership lookup failed", "roleCode", rule.FallbackRoleCode, "err", err)
		} else if len(fallbackUsers) > 0 {
			roleCode = rule.FallbackRoleCode
			users = fallbackUsers
		}
	}

	// Separation of duties: the case maker must not sit in the candidate
	// pool of a reviewing step when the rule demands it.
	if rule.RequireSeparationOfDuties && req.CreatedBy != "" {
		filtered := users[:0:0]
		for _, user := range users {
			if user != req.CreatedBy {
				filtered = append(filtered, user)
			}
		}
		users = filtered
	}

	// Delegations extend the pool: substitutes for current members inherit
	// the step candidates while their delegation is active.
	targets, err := r.source.ListActiveDelegationTargets(ctx, req.TenantID, roleCode, users)
	if err != nil {
		slog.Warn("assignment resolver: delegation lookup failed", "roleCode", roleCode, "err", err)
	} else {
		users = append(users, targets...)
	}

	return AssignmentResult{
		Resolved:       true,
		RoleCode:       roleCode,
		CandidateUsers: users,
		AssignmentMode: rule.AssignmentMode,
	}
}
