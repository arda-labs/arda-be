package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"

	"github.com/arda-labs/arda/apps/hrm-service/internal/domain"
)

type HRMRepository struct {
	db *sql.DB
}

func NewHRMRepository(db *sql.DB) *HRMRepository {
	return &HRMRepository{db: db}
}

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_fallback"
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func active(status string) string {
	if status == "" {
		return "active"
	}
	return status
}

func (r *HRMRepository) ListPositions(ctx context.Context, status, q string) ([]domain.Position, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, status, is_manager, description, created_at, updated_at
		FROM hrm_positions
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR code ILIKE '%' || $2 || '%' OR name ILIKE '%' || $2 || '%')
		ORDER BY code`, status, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Position, 0)
	for rows.Next() {
		var item domain.Position
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Status, &item.IsManager, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreatePosition(ctx context.Context, item domain.Position) (domain.Position, error) {
	if item.ID == "" {
		item.ID = newID("pos")
	}
	item.Status = active(item.Status)
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_positions (id, code, name, status, is_manager, description)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, code, name, status, is_manager, description, created_at, updated_at`,
		item.ID, item.Code, item.Name, item.Status, item.IsManager, item.Description,
	).Scan(&item.ID, &item.Code, &item.Name, &item.Status, &item.IsManager, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdatePosition(ctx context.Context, item domain.Position) (domain.Position, error) {
	item.Status = active(item.Status)
	err := r.db.QueryRowContext(ctx, `
		UPDATE hrm_positions
		SET code = $2, name = $3, status = $4, is_manager = $5, description = $6, updated_at = now()
		WHERE id = $1
		RETURNING id, code, name, status, is_manager, description, created_at, updated_at`,
		item.ID, item.Code, item.Name, item.Status, item.IsManager, item.Description,
	).Scan(&item.ID, &item.Code, &item.Name, &item.Status, &item.IsManager, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeletePosition(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM hrm_positions WHERE id = $1`, id)
	return err
}

func (r *HRMRepository) ListJobTitles(ctx context.Context, q string) ([]domain.JobTitle, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, description, created_at, updated_at
		FROM hrm_job_titles
		WHERE $1 = '' OR code ILIKE '%' || $1 || '%' OR name ILIKE '%' || $1 || '%'
		ORDER BY code`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.JobTitle, 0)
	for rows.Next() {
		var item domain.JobTitle
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreateJobTitle(ctx context.Context, item domain.JobTitle) (domain.JobTitle, error) {
	if item.ID == "" {
		item.ID = newID("title")
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_job_titles (id, code, name, description)
		VALUES ($1, $2, $3, $4)
		RETURNING id, code, name, description, created_at, updated_at`,
		item.ID, item.Code, item.Name, item.Description,
	).Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdateJobTitle(ctx context.Context, item domain.JobTitle) (domain.JobTitle, error) {
	err := r.db.QueryRowContext(ctx, `
		UPDATE hrm_job_titles
		SET code = $2, name = $3, description = $4, updated_at = now()
		WHERE id = $1
		RETURNING id, code, name, description, created_at, updated_at`,
		item.ID, item.Code, item.Name, item.Description,
	).Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeleteJobTitle(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM hrm_job_titles WHERE id = $1`, id)
	return err
}

func (r *HRMRepository) ListOrgUnits(ctx context.Context, organizationID, status, q string) ([]domain.OrgUnit, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, organization_id, name, org_level, parent_id, department_type, status, description, created_at, updated_at
		FROM hrm_org_units
		WHERE ($1 = '' OR organization_id = $1)
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR code ILIKE '%' || $3 || '%' OR name ILIKE '%' || $3 || '%')
		ORDER BY parent_id NULLS FIRST, code`, organizationID, status, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.OrgUnit, 0)
	for rows.Next() {
		var item domain.OrgUnit
		if err := rows.Scan(&item.ID, &item.Code, &item.OrganizationID, &item.Name, &item.OrgLevel, &item.ParentID, &item.DepartmentType, &item.Status, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreateOrgUnit(ctx context.Context, item domain.OrgUnit) (domain.OrgUnit, error) {
	if item.ID == "" {
		item.ID = newID("orgunit")
	}
	item.Status = active(item.Status)
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_org_units (id, code, organization_id, name, org_level, parent_id, department_type, status, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, code, organization_id, name, org_level, parent_id, department_type, status, description, created_at, updated_at`,
		item.ID, item.Code, item.OrganizationID, item.Name, item.OrgLevel, item.ParentID, item.DepartmentType, item.Status, item.Description,
	).Scan(&item.ID, &item.Code, &item.OrganizationID, &item.Name, &item.OrgLevel, &item.ParentID, &item.DepartmentType, &item.Status, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdateOrgUnit(ctx context.Context, item domain.OrgUnit) (domain.OrgUnit, error) {
	item.Status = active(item.Status)
	err := r.db.QueryRowContext(ctx, `
		UPDATE hrm_org_units
		SET code = $2, organization_id = $3, name = $4, org_level = $5, parent_id = $6,
			department_type = $7, status = $8, description = $9, updated_at = now()
		WHERE id = $1
		RETURNING id, code, organization_id, name, org_level, parent_id, department_type, status, description, created_at, updated_at`,
		item.ID, item.Code, item.OrganizationID, item.Name, item.OrgLevel, item.ParentID, item.DepartmentType, item.Status, item.Description,
	).Scan(&item.ID, &item.Code, &item.OrganizationID, &item.Name, &item.OrgLevel, &item.ParentID, &item.DepartmentType, &item.Status, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeleteOrgUnit(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM hrm_org_units WHERE id = $1`, id)
	return err
}

func (r *HRMRepository) ListEmployees(ctx context.Context, q string) ([]domain.Employee, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, employee_code, full_name, org_unit_id, position_id, job_title_id, iam_user_id, status, created_at, updated_at
		FROM hrm_employees
		WHERE $1 = '' OR employee_code ILIKE '%' || $1 || '%' OR full_name ILIKE '%' || $1 || '%'
		ORDER BY employee_code`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Employee, 0)
	for rows.Next() {
		var item domain.Employee
		if err := rows.Scan(&item.ID, &item.EmployeeCode, &item.FullName, &item.OrgUnitID, &item.PositionID, &item.JobTitleID, &item.IAMUserID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) ListEmployeeRegistrations(ctx context.Context, status string) ([]domain.EmployeeRegistration, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at
		FROM hrm_employee_registrations
		WHERE $1 = '' OR status = $1
		ORDER BY updated_at DESC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.EmployeeRegistration, 0)
	for rows.Next() {
		var item domain.EmployeeRegistration
		if err := rows.Scan(&item.ID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreateEmployeeRegistration(ctx context.Context, item domain.EmployeeRegistration) (domain.EmployeeRegistration, error) {
	if item.ID == "" {
		item.ID = newID("empreg")
	}
	if item.Status == "" {
		item.Status = "draft"
	}
	if item.Payload == "" {
		item.Payload = "{}"
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_employee_registrations (id, registration_code, payload, workflow_case_id, status, created_by)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6)
		RETURNING id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at`,
		item.ID, item.RegistrationCode, item.Payload, item.WorkflowCaseID, item.Status, item.CreatedBy,
	).Scan(&item.ID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) GetEmployeeRegistration(ctx context.Context, id string) (domain.EmployeeRegistration, error) {
	var item domain.EmployeeRegistration
	err := r.db.QueryRowContext(ctx, `
		SELECT id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at
		FROM hrm_employee_registrations
		WHERE id = $1`, id,
	).Scan(&item.ID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) SubmitEmployeeRegistration(ctx context.Context, id, workflowCaseID string) (domain.EmployeeRegistration, error) {
	var caseID *string
	if workflowCaseID != "" {
		caseID = &workflowCaseID
	}
	var item domain.EmployeeRegistration
	err := r.db.QueryRowContext(ctx, `
		UPDATE hrm_employee_registrations
		SET status = 'submitted', workflow_case_id = COALESCE($2, workflow_case_id), updated_at = now()
		WHERE id = $1
		RETURNING id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at`,
		id, caseID,
	).Scan(&item.ID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}
