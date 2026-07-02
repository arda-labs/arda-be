package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type WorkflowRoleCatalog struct {
	RoleCode          string    `json:"roleCode"`
	RoleName          string    `json:"roleName"`
	RoleType          string    `json:"roleType"`
	BusinessSubsystem string    `json:"businessSubsystem"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type WorkflowRoleMembership struct {
	ID            string     `json:"id"`
	RoleCode      string     `json:"roleCode"`
	PrincipalType string     `json:"principalType"`
	PrincipalID   string     `json:"principalId"`
	TenantID      string     `json:"tenantId"`
	OrgID         string     `json:"orgId"`
	BranchID      string     `json:"branchId"`
	ProductCode   string     `json:"productCode"`
	MinAmount     *float64   `json:"minAmount,omitempty"`
	MaxAmount     *float64   `json:"maxAmount,omitempty"`
	EffectiveFrom *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo   *time.Time `json:"effectiveTo,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type WorkflowAssignmentRule struct {
	ID                        string    `json:"id"`
	CaseType                  string    `json:"caseType"`
	StepCode                  string    `json:"stepCode"`
	RoleCode                  string    `json:"roleCode"`
	AssignmentMode            string    `json:"assignmentMode"`
	RequireSeparationOfDuties bool      `json:"requireSeparationOfDuties"`
	FallbackRoleCode          string    `json:"fallbackRoleCode"`
	Priority                  int       `json:"priority"`
	Status                    string    `json:"status"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

type WorkflowDelegation struct {
	ID              string     `json:"id"`
	FromPrincipalID string     `json:"fromPrincipalId"`
	ToPrincipalID   string     `json:"toPrincipalId"`
	RoleCode        string     `json:"roleCode"`
	EffectiveFrom   *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo     *time.Time `json:"effectiveTo,omitempty"`
	Reason          string     `json:"reason"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

func (r *CaseRepository) ListWorkflowRoleCatalog(ctx context.Context) ([]WorkflowRoleCatalog, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT role_code, role_name, role_type, business_subsystem, status, created_at, updated_at
		FROM workflow_role_catalog
		ORDER BY business_subsystem, role_type, role_code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WorkflowRoleCatalog
	for rows.Next() {
		item, err := scanWorkflowRoleCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateWorkflowRoleCatalog(ctx context.Context, in WorkflowRoleCatalog) (*WorkflowRoleCatalog, error) {
	if err := validateWorkflowRoleCatalog(in); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING role_code, role_name, role_type, business_subsystem, status, created_at, updated_at
	`, in.RoleCode, in.RoleName, in.RoleType, in.BusinessSubsystem, statusOrActive(in.Status))
	item, err := scanWorkflowRoleCatalog(row)
	return &item, err
}

func (r *CaseRepository) UpdateWorkflowRoleCatalog(ctx context.Context, roleCode string, in WorkflowRoleCatalog) (*WorkflowRoleCatalog, error) {
	if roleCode == "" {
		return nil, errors.New("roleCode is required")
	}
	if err := validateWorkflowRoleCatalog(WorkflowRoleCatalog{RoleCode: roleCode, RoleName: in.RoleName, RoleType: in.RoleType, BusinessSubsystem: in.BusinessSubsystem}); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE workflow_role_catalog
		SET role_name = $2, role_type = $3, business_subsystem = $4, status = $5,
		    updated_at = CURRENT_TIMESTAMP
		WHERE role_code = $1
		RETURNING role_code, role_name, role_type, business_subsystem, status, created_at, updated_at
	`, roleCode, in.RoleName, in.RoleType, in.BusinessSubsystem, statusOrActive(in.Status))
	item, err := scanWorkflowRoleCatalog(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (r *CaseRepository) ListWorkflowRoleMemberships(ctx context.Context) ([]WorkflowRoleMembership, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, role_code, principal_type, principal_id, tenant_id, org_id, branch_id,
		       product_code, min_amount, max_amount, effective_from, effective_to,
		       status, created_at, updated_at
		FROM workflow_role_memberships
		ORDER BY role_code, principal_type, principal_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WorkflowRoleMembership
	for rows.Next() {
		item, err := scanWorkflowRoleMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateWorkflowRoleMembership(ctx context.Context, in WorkflowRoleMembership) (*WorkflowRoleMembership, error) {
	if err := validateWorkflowRoleMembership(in); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO workflow_role_memberships (
			id, role_code, principal_type, principal_id, tenant_id, org_id, branch_id,
			product_code, min_amount, max_amount, effective_from, effective_to, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, CURRENT_TIMESTAMP), $12, $13)
		RETURNING id, role_code, principal_type, principal_id, tenant_id, org_id, branch_id,
		          product_code, min_amount, max_amount, effective_from, effective_to,
		          status, created_at, updated_at
	`, id, in.RoleCode, in.PrincipalType, in.PrincipalID, in.TenantID, in.OrgID, in.BranchID,
		in.ProductCode, in.MinAmount, in.MaxAmount, in.EffectiveFrom, in.EffectiveTo, statusOrActive(in.Status))
	item, err := scanWorkflowRoleMembership(row)
	return &item, err
}

func (r *CaseRepository) UpdateWorkflowRoleMembership(ctx context.Context, id string, in WorkflowRoleMembership) (*WorkflowRoleMembership, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if err := validateWorkflowRoleMembership(in); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE workflow_role_memberships
		SET role_code = $2, principal_type = $3, principal_id = $4, tenant_id = $5,
		    org_id = $6, branch_id = $7, product_code = $8, min_amount = $9,
		    max_amount = $10, effective_from = COALESCE($11, effective_from),
		    effective_to = $12, status = $13, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, role_code, principal_type, principal_id, tenant_id, org_id, branch_id,
		          product_code, min_amount, max_amount, effective_from, effective_to,
		          status, created_at, updated_at
	`, id, in.RoleCode, in.PrincipalType, in.PrincipalID, in.TenantID, in.OrgID, in.BranchID,
		in.ProductCode, in.MinAmount, in.MaxAmount, in.EffectiveFrom, in.EffectiveTo, statusOrActive(in.Status))
	item, err := scanWorkflowRoleMembership(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (r *CaseRepository) ListWorkflowAssignmentRules(ctx context.Context) ([]WorkflowAssignmentRule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, case_type, step_code, role_code, assignment_mode,
		       require_separation_of_duties, fallback_role_code, priority,
		       status, created_at, updated_at
		FROM workflow_assignment_rules
		ORDER BY case_type, step_code, priority
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WorkflowAssignmentRule
	for rows.Next() {
		item, err := scanWorkflowAssignmentRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateWorkflowAssignmentRule(ctx context.Context, in WorkflowAssignmentRule) (*WorkflowAssignmentRule, error) {
	if err := validateWorkflowAssignmentRule(in); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO workflow_assignment_rules (
			id, case_type, step_code, role_code, assignment_mode,
			require_separation_of_duties, fallback_role_code, priority, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, case_type, step_code, role_code, assignment_mode,
		          require_separation_of_duties, fallback_role_code, priority,
		          status, created_at, updated_at
	`, id, in.CaseType, in.StepCode, in.RoleCode, in.AssignmentMode,
		in.RequireSeparationOfDuties, in.FallbackRoleCode, priorityOrDefault(in.Priority), statusOrActive(in.Status))
	item, err := scanWorkflowAssignmentRule(row)
	return &item, err
}

func (r *CaseRepository) UpdateWorkflowAssignmentRule(ctx context.Context, id string, in WorkflowAssignmentRule) (*WorkflowAssignmentRule, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if err := validateWorkflowAssignmentRule(in); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE workflow_assignment_rules
		SET case_type = $2, step_code = $3, role_code = $4, assignment_mode = $5,
		    require_separation_of_duties = $6, fallback_role_code = $7,
		    priority = $8, status = $9, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, case_type, step_code, role_code, assignment_mode,
		          require_separation_of_duties, fallback_role_code, priority,
		          status, created_at, updated_at
	`, id, in.CaseType, in.StepCode, in.RoleCode, in.AssignmentMode,
		in.RequireSeparationOfDuties, in.FallbackRoleCode, priorityOrDefault(in.Priority), statusOrActive(in.Status))
	item, err := scanWorkflowAssignmentRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (r *CaseRepository) ListWorkflowDelegations(ctx context.Context) ([]WorkflowDelegation, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, from_principal_id, to_principal_id, role_code, effective_from,
		       effective_to, reason, status, created_at, updated_at
		FROM workflow_delegations
		ORDER BY role_code, effective_from DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WorkflowDelegation
	for rows.Next() {
		item, err := scanWorkflowDelegation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateWorkflowDelegation(ctx context.Context, in WorkflowDelegation) (*WorkflowDelegation, error) {
	if err := validateWorkflowDelegation(in); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO workflow_delegations (
			id, from_principal_id, to_principal_id, role_code, effective_from,
			effective_to, reason, status
		)
		VALUES ($1, $2, $3, $4, COALESCE($5, CURRENT_TIMESTAMP), $6, $7, $8)
		RETURNING id, from_principal_id, to_principal_id, role_code, effective_from,
		          effective_to, reason, status, created_at, updated_at
	`, id, in.FromPrincipalID, in.ToPrincipalID, in.RoleCode, in.EffectiveFrom,
		in.EffectiveTo, in.Reason, statusOrActive(in.Status))
	item, err := scanWorkflowDelegation(row)
	return &item, err
}

func (r *CaseRepository) UpdateWorkflowDelegation(ctx context.Context, id string, in WorkflowDelegation) (*WorkflowDelegation, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if err := validateWorkflowDelegation(in); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE workflow_delegations
		SET from_principal_id = $2, to_principal_id = $3, role_code = $4,
		    effective_from = COALESCE($5, effective_from), effective_to = $6,
		    reason = $7, status = $8, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, from_principal_id, to_principal_id, role_code, effective_from,
		          effective_to, reason, status, created_at, updated_at
	`, id, in.FromPrincipalID, in.ToPrincipalID, in.RoleCode, in.EffectiveFrom,
		in.EffectiveTo, in.Reason, statusOrActive(in.Status))
	item, err := scanWorkflowDelegation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func validateWorkflowRoleCatalog(in WorkflowRoleCatalog) error {
	switch {
	case in.RoleCode == "":
		return errors.New("roleCode is required")
	case in.RoleName == "":
		return errors.New("roleName is required")
	case in.RoleType == "":
		return errors.New("roleType is required")
	case in.BusinessSubsystem == "":
		return errors.New("businessSubsystem is required")
	default:
		return nil
	}
}

func validateWorkflowRoleMembership(in WorkflowRoleMembership) error {
	switch {
	case in.RoleCode == "":
		return errors.New("roleCode is required")
	case in.PrincipalType != "USER" && in.PrincipalType != "GROUP":
		return errors.New("principalType must be USER or GROUP")
	case in.PrincipalID == "":
		return errors.New("principalId is required")
	default:
		return nil
	}
}

func validateWorkflowAssignmentRule(in WorkflowAssignmentRule) error {
	switch {
	case in.CaseType == "":
		return errors.New("caseType is required")
	case in.StepCode == "":
		return errors.New("stepCode is required")
	case in.RoleCode == "":
		return errors.New("roleCode is required")
	case in.AssignmentMode == "":
		return errors.New("assignmentMode is required")
	default:
		return nil
	}
}

func validateWorkflowDelegation(in WorkflowDelegation) error {
	switch {
	case in.FromPrincipalID == "":
		return errors.New("fromPrincipalId is required")
	case in.ToPrincipalID == "":
		return errors.New("toPrincipalId is required")
	case in.RoleCode == "":
		return errors.New("roleCode is required")
	default:
		return nil
	}
}

func scanWorkflowRoleCatalog(s scanner) (WorkflowRoleCatalog, error) {
	var item WorkflowRoleCatalog
	err := s.Scan(&item.RoleCode, &item.RoleName, &item.RoleType,
		&item.BusinessSubsystem, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanWorkflowRoleMembership(s scanner) (WorkflowRoleMembership, error) {
	var item WorkflowRoleMembership
	var minAmount, maxAmount sql.NullFloat64
	var effectiveFrom, effectiveTo sql.NullTime
	err := s.Scan(&item.ID, &item.RoleCode, &item.PrincipalType, &item.PrincipalID,
		&item.TenantID, &item.OrgID, &item.BranchID, &item.ProductCode,
		&minAmount, &maxAmount, &effectiveFrom, &effectiveTo,
		&item.Status, &item.CreatedAt, &item.UpdatedAt)
	item.MinAmount = nullFloatPtr(minAmount)
	item.MaxAmount = nullFloatPtr(maxAmount)
	if effectiveFrom.Valid {
		item.EffectiveFrom = &effectiveFrom.Time
	}
	if effectiveTo.Valid {
		item.EffectiveTo = &effectiveTo.Time
	}
	return item, err
}

func scanWorkflowAssignmentRule(s scanner) (WorkflowAssignmentRule, error) {
	var item WorkflowAssignmentRule
	err := s.Scan(&item.ID, &item.CaseType, &item.StepCode, &item.RoleCode,
		&item.AssignmentMode, &item.RequireSeparationOfDuties, &item.FallbackRoleCode,
		&item.Priority, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanWorkflowDelegation(s scanner) (WorkflowDelegation, error) {
	var item WorkflowDelegation
	var effectiveFrom, effectiveTo sql.NullTime
	err := s.Scan(&item.ID, &item.FromPrincipalID, &item.ToPrincipalID, &item.RoleCode,
		&effectiveFrom, &effectiveTo, &item.Reason, &item.Status,
		&item.CreatedAt, &item.UpdatedAt)
	if effectiveFrom.Valid {
		item.EffectiveFrom = &effectiveFrom.Time
	}
	if effectiveTo.Valid {
		item.EffectiveTo = &effectiveTo.Time
	}
	return item, err
}

func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func priorityOrDefault(priority int) int {
	if priority <= 0 {
		return 100
	}
	return priority
}
