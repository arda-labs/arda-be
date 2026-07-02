package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type ProcessConfigUpdate struct {
	BpmnProcessID      string `json:"bpmnProcessId"`
	BpmnVersion        int    `json:"bpmnVersion"`
	WorkflowEnabled    bool   `json:"workflowEnabled"`
	DefaultSLAPolicyID string `json:"defaultSlaPolicyId"`
	MakerRole          string `json:"makerRole"`
	CheckerRole        string `json:"checkerRole"`
	Status             string `json:"status"`
}

type SLAPolicy struct {
	ID             string          `json:"id"`
	Code           string          `json:"code"`
	Name           string          `json:"name"`
	CaseType       string          `json:"caseType"`
	DueInHours     int             `json:"dueInHours"`
	WarningInHours int             `json:"warningInHours"`
	EscalationRole string          `json:"escalationRole"`
	Status         string          `json:"status"`
	EffectiveFrom  *time.Time      `json:"effectiveFrom,omitempty"`
	EffectiveTo    *time.Time      `json:"effectiveTo,omitempty"`
	TaskPolicies   []SLATaskPolicy `json:"taskPolicies,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type SLATaskPolicy struct {
	ID             string     `json:"id"`
	SLAPolicyID    string     `json:"slaPolicyId"`
	StepCode       string     `json:"stepCode"`
	TaskName       string     `json:"taskName"`
	DurationValue  int        `json:"durationValue"`
	DurationUnit   string     `json:"durationUnit"`
	WarningMode    string     `json:"warningMode"`
	WarningValue   int        `json:"warningValue"`
	WarningUnit    string     `json:"warningUnit"`
	EscalationRole string     `json:"escalationRole"`
	SortOrder      int        `json:"sortOrder"`
	Status         string     `json:"status"`
	EffectiveFrom  *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo    *time.Time `json:"effectiveTo,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type DescriptionTemplate struct {
	ID                string    `json:"id"`
	Code              string    `json:"code"`
	BusinessSubsystem string    `json:"businessSubsystem"`
	CaseType          string    `json:"caseType"`
	Pattern           string    `json:"pattern"`
	Preview           string    `json:"preview"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type ProcessRole struct {
	ID           string    `json:"id"`
	CaseType     string    `json:"caseType"`
	StepCode     string    `json:"stepCode"`
	BusinessRole string    `json:"businessRole"`
	IAMRole      string    `json:"iamRole"`
	ActionScope  string    `json:"actionScope"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (r *CaseRepository) UpdateProcessConfig(ctx context.Context, caseType string, in ProcessConfigUpdate) (*CaseType, error) {
	if caseType == "" {
		return nil, errors.New("caseType is required")
	}
	if in.BpmnProcessID == "" || in.MakerRole == "" || in.CheckerRole == "" {
		return nil, errors.New("bpmnProcessId, makerRole and checkerRole are required")
	}
	if in.BpmnVersion <= 0 {
		in.BpmnVersion = 1
	}
	if in.Status == "" {
		in.Status = "ACTIVE"
	}

	row := r.db.QueryRowContext(ctx, `
		UPDATE business_operation_types
		SET bpmn_process_id = $2, bpmn_version = $3, workflow_enabled = $4,
		    default_sla_policy_id = NULLIF($5, ''), maker_role = $6,
		    checker_role = $7, status = $8, updated_at = CURRENT_TIMESTAMP
		WHERE case_type = $1
		RETURNING case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
		          workflow_enabled, default_sla_policy_id, maker_role, checker_role,
		          owner_service, status, effective_from, effective_to
	`, caseType, in.BpmnProcessID, in.BpmnVersion, in.WorkflowEnabled,
		in.DefaultSLAPolicyID, in.MakerRole, in.CheckerRole, in.Status)

	ct, err := scanCaseType(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ct, nil
}

func (r *CaseRepository) ListSLAPolicies(ctx context.Context) ([]SLAPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, case_type, due_in_hours, warning_in_hours,
		       escalation_role, status, effective_from, effective_to, created_at, updated_at
		FROM business_sla_policies
		ORDER BY case_type, code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SLAPolicy
	for rows.Next() {
		item, err := scanSLAPolicy(rows)
		if err != nil {
			return nil, err
		}
		item.TaskPolicies, err = r.ListSLATaskPolicies(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) ListSLATaskPolicies(ctx context.Context, slaPolicyID string) ([]SLATaskPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
		       warning_mode, warning_value, warning_unit, escalation_role, sort_order,
		       status, effective_from, effective_to, created_at, updated_at
		FROM business_sla_task_policies
		WHERE sla_policy_id = $1
		ORDER BY sort_order, step_code
	`, slaPolicyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SLATaskPolicy
	for rows.Next() {
		item, err := scanSLATaskPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateSLAPolicy(ctx context.Context, in SLAPolicy) (*SLAPolicy, error) {
	normalizeSLASummary(&in)
	if err := validateSLAPolicy(in); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		INSERT INTO business_sla_policies (
			id, code, name, case_type, due_in_hours, warning_in_hours,
			escalation_role, status, effective_from, effective_to
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9, CURRENT_TIMESTAMP), $10)
		RETURNING id, code, name, case_type, due_in_hours, warning_in_hours,
		          escalation_role, status, effective_from, effective_to, created_at, updated_at
	`, id, in.Code, in.Name, in.CaseType, in.DueInHours, in.WarningInHours,
		in.EscalationRole, statusOrActive(in.Status), in.EffectiveFrom, in.EffectiveTo)
	item, err := scanSLAPolicy(row)
	if err != nil {
		return nil, err
	}
	if err := replaceSLATaskPolicies(ctx, tx, item.ID, in.TaskPolicies); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	item.TaskPolicies, err = r.ListSLATaskPolicies(ctx, item.ID)
	return &item, err
}

func (r *CaseRepository) UpdateSLAPolicy(ctx context.Context, id string, in SLAPolicy) (*SLAPolicy, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	normalizeSLASummary(&in)
	if err := validateSLAPolicy(in); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		UPDATE business_sla_policies
		SET code = $2, name = $3, case_type = $4, due_in_hours = $5,
		    warning_in_hours = $6, escalation_role = $7, status = $8,
		    effective_from = COALESCE($9, effective_from), effective_to = $10,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, code, name, case_type, due_in_hours, warning_in_hours,
		          escalation_role, status, effective_from, effective_to, created_at, updated_at
	`, id, in.Code, in.Name, in.CaseType, in.DueInHours, in.WarningInHours,
		in.EscalationRole, statusOrActive(in.Status), in.EffectiveFrom, in.EffectiveTo)
	item, err := scanSLAPolicy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := replaceSLATaskPolicies(ctx, tx, item.ID, in.TaskPolicies); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	item.TaskPolicies, err = r.ListSLATaskPolicies(ctx, item.ID)
	return &item, err
}

func (r *CaseRepository) ListDescriptionTemplates(ctx context.Context) ([]DescriptionTemplate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, business_subsystem, case_type, pattern, preview, status, created_at, updated_at
		FROM business_description_templates
		ORDER BY business_subsystem, case_type, code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DescriptionTemplate
	for rows.Next() {
		item, err := scanDescriptionTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateDescriptionTemplate(ctx context.Context, in DescriptionTemplate) (*DescriptionTemplate, error) {
	if err := validateDescriptionTemplate(in); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO business_description_templates (
			id, code, business_subsystem, case_type, pattern, preview, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, code, business_subsystem, case_type, pattern, preview, status, created_at, updated_at
	`, id, in.Code, in.BusinessSubsystem, in.CaseType, in.Pattern, in.Preview, statusOrActive(in.Status))
	item, err := scanDescriptionTemplate(row)
	return &item, err
}

func (r *CaseRepository) UpdateDescriptionTemplate(ctx context.Context, id string, in DescriptionTemplate) (*DescriptionTemplate, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if err := validateDescriptionTemplate(in); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE business_description_templates
		SET code = $2, business_subsystem = $3, case_type = $4, pattern = $5, preview = $6,
		    status = $7, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, code, business_subsystem, case_type, pattern, preview, status, created_at, updated_at
	`, id, in.Code, in.BusinessSubsystem, in.CaseType, in.Pattern, in.Preview, statusOrActive(in.Status))
	item, err := scanDescriptionTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (r *CaseRepository) ListProcessRoles(ctx context.Context) ([]ProcessRole, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, case_type, step_code, business_role, iam_role, action_scope,
		       status, created_at, updated_at
		FROM business_process_roles
		ORDER BY case_type, step_code, business_role
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProcessRole
	for rows.Next() {
		item, err := scanProcessRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CaseRepository) CreateProcessRole(ctx context.Context, in ProcessRole) (*ProcessRole, error) {
	if err := validateProcessRole(in); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO business_process_roles (
			id, case_type, step_code, business_role, iam_role, action_scope, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, case_type, step_code, business_role, iam_role, action_scope,
		          status, created_at, updated_at
	`, id, in.CaseType, in.StepCode, in.BusinessRole, in.IAMRole, in.ActionScope, statusOrActive(in.Status))
	item, err := scanProcessRole(row)
	return &item, err
}

func (r *CaseRepository) UpdateProcessRole(ctx context.Context, id string, in ProcessRole) (*ProcessRole, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if err := validateProcessRole(in); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE business_process_roles
		SET case_type = $2, step_code = $3, business_role = $4, iam_role = $5,
		    action_scope = $6, status = $7, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, case_type, step_code, business_role, iam_role, action_scope,
		          status, created_at, updated_at
	`, id, in.CaseType, in.StepCode, in.BusinessRole, in.IAMRole, in.ActionScope, statusOrActive(in.Status))
	item, err := scanProcessRole(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func validateSLAPolicy(in SLAPolicy) error {
	switch {
	case in.Code == "":
		return errors.New("code is required")
	case in.Name == "":
		return errors.New("name is required")
	case in.CaseType == "":
		return errors.New("caseType is required")
	case in.DueInHours <= 0:
		return errors.New("dueInHours must be positive")
	case in.WarningInHours < 0:
		return errors.New("warningInHours must not be negative")
	case in.WarningInHours >= in.DueInHours:
		return errors.New("warningInHours must be less than dueInHours")
	case in.EscalationRole == "":
		return errors.New("escalationRole is required")
	}
	for _, task := range in.TaskPolicies {
		if err := validateSLATaskPolicy(task); err != nil {
			return err
		}
	}
	return nil
}

func validateSLATaskPolicy(in SLATaskPolicy) error {
	switch {
	case in.StepCode == "":
		return errors.New("task stepCode is required")
	case in.TaskName == "":
		return errors.New("taskName is required")
	case in.DurationValue <= 0:
		return errors.New("durationValue must be positive")
	case in.DurationUnit != "MINUTE" && in.DurationUnit != "HOUR":
		return errors.New("durationUnit must be MINUTE or HOUR")
	case in.WarningMode != "ABSOLUTE" && in.WarningMode != "PERCENT":
		return errors.New("warningMode must be ABSOLUTE or PERCENT")
	case in.WarningValue < 0:
		return errors.New("warningValue must not be negative")
	case in.WarningMode == "PERCENT" && in.WarningValue > 100:
		return errors.New("warningValue percent must be at most 100")
	case in.WarningUnit != "MINUTE" && in.WarningUnit != "HOUR" && in.WarningUnit != "PERCENT":
		return errors.New("warningUnit must be MINUTE, HOUR or PERCENT")
	case in.EscalationRole == "":
		return errors.New("task escalationRole is required")
	default:
		return nil
	}
}

func validateDescriptionTemplate(in DescriptionTemplate) error {
	switch {
	case in.Code == "":
		return errors.New("code is required")
	case in.BusinessSubsystem == "":
		return errors.New("businessSubsystem is required")
	case in.CaseType == "":
		return errors.New("caseType is required")
	case in.Pattern == "":
		return errors.New("pattern is required")
	default:
		return nil
	}
}

func validateProcessRole(in ProcessRole) error {
	switch {
	case in.CaseType == "":
		return errors.New("caseType is required")
	case in.StepCode == "":
		return errors.New("stepCode is required")
	case in.BusinessRole == "":
		return errors.New("businessRole is required")
	case in.IAMRole == "":
		return errors.New("iamRole is required")
	case in.ActionScope == "":
		return errors.New("actionScope is required")
	default:
		return nil
	}
}

func scanSLAPolicy(s scanner) (SLAPolicy, error) {
	var item SLAPolicy
	var effectiveFrom, effectiveTo sql.NullTime
	err := s.Scan(&item.ID, &item.Code, &item.Name, &item.CaseType,
		&item.DueInHours, &item.WarningInHours, &item.EscalationRole,
		&item.Status, &effectiveFrom, &effectiveTo, &item.CreatedAt, &item.UpdatedAt)
	if effectiveFrom.Valid {
		item.EffectiveFrom = &effectiveFrom.Time
	}
	if effectiveTo.Valid {
		item.EffectiveTo = &effectiveTo.Time
	}
	return item, err
}

func scanSLATaskPolicy(s scanner) (SLATaskPolicy, error) {
	var item SLATaskPolicy
	var effectiveFrom, effectiveTo sql.NullTime
	err := s.Scan(&item.ID, &item.SLAPolicyID, &item.StepCode, &item.TaskName,
		&item.DurationValue, &item.DurationUnit, &item.WarningMode, &item.WarningValue,
		&item.WarningUnit, &item.EscalationRole, &item.SortOrder, &item.Status,
		&effectiveFrom, &effectiveTo, &item.CreatedAt, &item.UpdatedAt)
	if effectiveFrom.Valid {
		item.EffectiveFrom = &effectiveFrom.Time
	}
	if effectiveTo.Valid {
		item.EffectiveTo = &effectiveTo.Time
	}
	return item, err
}

func scanDescriptionTemplate(s scanner) (DescriptionTemplate, error) {
	var item DescriptionTemplate
	err := s.Scan(&item.ID, &item.Code, &item.BusinessSubsystem, &item.CaseType, &item.Pattern,
		&item.Preview, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanProcessRole(s scanner) (ProcessRole, error) {
	var item ProcessRole
	err := s.Scan(&item.ID, &item.CaseType, &item.StepCode, &item.BusinessRole,
		&item.IAMRole, &item.ActionScope, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func statusOrActive(status string) string {
	if status == "" {
		return "ACTIVE"
	}
	return status
}

func replaceSLATaskPolicies(ctx context.Context, tx *sql.Tx, slaPolicyID string, items []SLATaskPolicy) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM business_sla_task_policies WHERE sla_policy_id = $1`, slaPolicyID); err != nil {
		return err
	}
	for i, item := range items {
		if item.SortOrder == 0 {
			item.SortOrder = (i + 1) * 10
		}
		if item.Status == "" {
			item.Status = "ACTIVE"
		}
		id := item.ID
		if id == "" {
			var err error
			id, err = newID()
			if err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO business_sla_task_policies (
				id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
				warning_mode, warning_value, warning_unit, escalation_role,
				sort_order, status, effective_from, effective_to
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, COALESCE($13, CURRENT_TIMESTAMP), $14)
		`, id, slaPolicyID, item.StepCode, item.TaskName, item.DurationValue, item.DurationUnit,
			item.WarningMode, item.WarningValue, item.WarningUnit, item.EscalationRole,
			item.SortOrder, item.Status, item.EffectiveFrom, item.EffectiveTo)
		if err != nil {
			return err
		}
	}
	return nil
}

func normalizeSLASummary(in *SLAPolicy) {
	if in.DueInHours <= 0 && len(in.TaskPolicies) > 0 {
		for _, task := range in.TaskPolicies {
			hours := task.DurationValue
			if task.DurationUnit == "MINUTE" {
				hours = (task.DurationValue + 59) / 60
				if hours == 0 {
					hours = 1
				}
			}
			if hours > in.DueInHours {
				in.DueInHours = hours
			}
		}
	}
	if in.WarningInHours < 0 {
		in.WarningInHours = 0
	}
}
