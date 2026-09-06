package repository

import (
	"context"
	"strings"
)

// FindAssignmentRule returns the highest-priority ACTIVE assignment rule for a
// case-type/step pair, or nil when none is configured (caller falls back to
// whatever candidate groups the BPMN itself declares).
func (r *CaseRepository) FindAssignmentRule(ctx context.Context, caseType, stepCode string) (*WorkflowAssignmentRule, error) {
	if strings.TrimSpace(caseType) == "" || strings.TrimSpace(stepCode) == "" {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, case_type, step_code, role_code, assignment_mode,
		       require_separation_of_duties, fallback_role_code, priority,
		       status, created_at, updated_at
		FROM workflow_assignment_rules
		WHERE case_type = $1 AND step_code = $2 AND status = 'ACTIVE'
		ORDER BY priority ASC
		LIMIT 1
	`, caseType, stepCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	item, err := scanWorkflowAssignmentRule(rows)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListActiveMembershipUsers returns USER principals currently holding a role
// for a tenant: ACTIVE status and inside the membership effective window.
func (r *CaseRepository) ListActiveMembershipUsers(ctx context.Context, tenantID, roleCode string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT principal_id
		FROM workflow_role_memberships
		WHERE role_code = $1
		  AND tenant_id = $2
		  AND principal_type = 'USER'
		  AND status = 'ACTIVE'
		  AND effective_from <= CURRENT_TIMESTAMP
		  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP)
		ORDER BY principal_id
	`, roleCode, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]string, 0)
	for rows.Next() {
		var principal string
		if err := rows.Scan(&principal); err != nil {
			return nil, err
		}
		if strings.TrimSpace(principal) != "" {
			users = append(users, principal)
		}
	}
	return users, rows.Err()
}

// ListActiveDelegationTargets returns substitute users that currently take
// over duties of the given role members (ACTIVE delegations inside window).
func (r *CaseRepository) ListActiveDelegationTargets(ctx context.Context, tenantID, roleCode string, members []string) ([]string, error) {
	if len(members) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT to_principal_id
		FROM workflow_delegations
		WHERE role_code = $1
		  AND tenant_id = $2
		  AND status = 'ACTIVE'
		  AND effective_from <= CURRENT_TIMESTAMP
		  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP)
		  AND from_principal_id = ANY($3)
		ORDER BY to_principal_id
	`, roleCode, tenantID, members)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := make([]string, 0)
	for rows.Next() {
		var principal string
		if err := rows.Scan(&principal); err != nil {
			return nil, err
		}
		if strings.TrimSpace(principal) != "" {
			targets = append(targets, principal)
		}
	}
	return targets, rows.Err()
}
