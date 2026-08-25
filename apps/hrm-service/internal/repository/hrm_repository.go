package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/arda-labs/arda/apps/hrm-service/internal/domain"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
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
		panic("secure hrm id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func active(status string) string {
	if status == "" {
		return "active"
	}
	return status
}

func tenantID(ctx context.Context) (string, error) {
	tenant := ardametadata.FromOutgoing(ctx).TenantID
	if tenant == "" {
		return "", fmt.Errorf("verified tenant scope is required")
	}
	return tenant, nil
}

func (r *HRMRepository) ListPositions(ctx context.Context, status, q string) ([]domain.Position, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, status, is_manager, description, created_at, updated_at
		FROM hrm_positions
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR code ILIKE '%' || $3 || '%' OR name ILIKE '%' || $3 || '%')
		ORDER BY code`, tenant, status, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Position, 0)
	for rows.Next() {
		var item domain.Position
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Status, &item.IsManager, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreatePosition(ctx context.Context, item domain.Position) (domain.Position, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	if item.ID == "" {
		item.ID = newID("pos")
	}
	item.Status = active(item.Status)
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_positions (id, tenant_id, code, name, status, is_manager, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, tenant_id, code, name, status, is_manager, description, created_at, updated_at`,
		item.ID, item.TenantID, item.Code, item.Name, item.Status, item.IsManager, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Status, &item.IsManager, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdatePosition(ctx context.Context, item domain.Position) (domain.Position, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	item.Status = active(item.Status)
	err = r.db.QueryRowContext(ctx, `
		UPDATE hrm_positions
		SET code = $3, name = $4, status = $5, is_manager = $6, description = $7, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, code, name, status, is_manager, description, created_at, updated_at`,
		tenant, item.ID, item.Code, item.Name, item.Status, item.IsManager, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Status, &item.IsManager, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeletePosition(ctx context.Context, id string) error {
	tenant, err := tenantID(ctx)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM hrm_positions WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return err
}

func (r *HRMRepository) ListJobTitles(ctx context.Context, q string) ([]domain.JobTitle, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, description, created_at, updated_at
		FROM hrm_job_titles
		WHERE tenant_id = $1 AND ($2 = '' OR code ILIKE '%' || $2 || '%' OR name ILIKE '%' || $2 || '%')
		ORDER BY code`, tenant, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.JobTitle, 0)
	for rows.Next() {
		var item domain.JobTitle
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreateJobTitle(ctx context.Context, item domain.JobTitle) (domain.JobTitle, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	if item.ID == "" {
		item.ID = newID("title")
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_job_titles (id, tenant_id, code, name, description)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, code, name, description, created_at, updated_at`,
		item.ID, item.TenantID, item.Code, item.Name, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdateJobTitle(ctx context.Context, item domain.JobTitle) (domain.JobTitle, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	err = r.db.QueryRowContext(ctx, `
		UPDATE hrm_job_titles
		SET code = $3, name = $4, description = $5, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, code, name, description, created_at, updated_at`,
		tenant, item.ID, item.Code, item.Name, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeleteJobTitle(ctx context.Context, id string) error {
	tenant, err := tenantID(ctx)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM hrm_job_titles WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return err
}

func (r *HRMRepository) ListOrgUnits(ctx context.Context, organizationID, status, q string) ([]domain.OrgUnit, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, organization_id, name, org_level, parent_id, department_type, status, description, created_at, updated_at
		FROM hrm_org_units
		WHERE tenant_id = $1 AND ($2 = '' OR organization_id = $2)
		  AND ($3 = '' OR status = $3)
		  AND ($4 = '' OR code ILIKE '%' || $4 || '%' OR name ILIKE '%' || $4 || '%')
		ORDER BY parent_id NULLS FIRST, code`, tenant, organizationID, status, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.OrgUnit, 0)
	for rows.Next() {
		var item domain.OrgUnit
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.OrganizationID, &item.Name, &item.OrgLevel, &item.ParentID, &item.DepartmentType, &item.Status, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreateOrgUnit(ctx context.Context, item domain.OrgUnit) (domain.OrgUnit, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	if item.ID == "" {
		item.ID = newID("orgunit")
	}
	item.Status = active(item.Status)
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_org_units (id, tenant_id, code, organization_id, name, org_level, parent_id, department_type, status, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, code, organization_id, name, org_level, parent_id, department_type, status, description, created_at, updated_at`,
		item.ID, item.TenantID, item.Code, item.OrganizationID, item.Name, item.OrgLevel, item.ParentID, item.DepartmentType, item.Status, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.OrganizationID, &item.Name, &item.OrgLevel, &item.ParentID, &item.DepartmentType, &item.Status, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdateOrgUnit(ctx context.Context, item domain.OrgUnit) (domain.OrgUnit, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	item.Status = active(item.Status)
	err = r.db.QueryRowContext(ctx, `
		UPDATE hrm_org_units
		SET code = $3, organization_id = $4, name = $5, org_level = $6, parent_id = $7,
			department_type = $8, status = $9, description = $10, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, code, organization_id, name, org_level, parent_id, department_type, status, description, created_at, updated_at`,
		tenant, item.ID, item.Code, item.OrganizationID, item.Name, item.OrgLevel, item.ParentID, item.DepartmentType, item.Status, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.OrganizationID, &item.Name, &item.OrgLevel, &item.ParentID, &item.DepartmentType, &item.Status, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeleteOrgUnit(ctx context.Context, id string) error {
	tenant, err := tenantID(ctx)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM hrm_org_units WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return err
}

func (r *HRMRepository) ListEmployees(ctx context.Context, q string) ([]domain.Employee, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, employee_code, full_name, org_unit_id, position_id, job_title_id, iam_user_id, status, created_at, updated_at
		FROM hrm_employees
		WHERE tenant_id = $1 AND ($2 = '' OR employee_code ILIKE '%' || $2 || '%' OR full_name ILIKE '%' || $2 || '%')
		ORDER BY employee_code`, tenant, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Employee, 0)
	for rows.Next() {
		var item domain.Employee
		if err := rows.Scan(&item.ID, &item.TenantID, &item.EmployeeCode, &item.FullName, &item.OrgUnitID, &item.PositionID, &item.JobTitleID, &item.IAMUserID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) ListEmployeeRegistrations(ctx context.Context, status string) ([]domain.EmployeeRegistration, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at
		FROM hrm_employee_registrations
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY updated_at DESC`, tenant, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.EmployeeRegistration, 0)
	for rows.Next() {
		var item domain.EmployeeRegistration
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreateEmployeeRegistration(ctx context.Context, item domain.EmployeeRegistration) (domain.EmployeeRegistration, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	if item.ID == "" {
		item.ID = newID("empreg")
	}
	if item.RegistrationCode == "" {
		code, err := r.nextRegistrationCode(ctx)
		if err != nil {
			return item, err
		}
		item.RegistrationCode = code
	}
	if item.Status == "" {
		item.Status = "draft"
	}
	if item.Payload == "" {
		item.Payload = "{}"
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_employee_registrations (id, tenant_id, registration_code, payload, workflow_case_id, status, created_by)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)
		RETURNING id, tenant_id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at`,
		item.ID, item.TenantID, item.RegistrationCode, item.Payload, item.WorkflowCaseID, item.Status, item.CreatedBy,
	).Scan(&item.ID, &item.TenantID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) GetEmployeeRegistration(ctx context.Context, id string) (domain.EmployeeRegistration, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return domain.EmployeeRegistration{}, err
	}
	var item domain.EmployeeRegistration
	err = r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at
		FROM hrm_employee_registrations
		WHERE tenant_id = $1 AND id = $2`, tenant, id,
	).Scan(&item.ID, &item.TenantID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdateEmployeeRegistration(ctx context.Context, id, payload string) (domain.EmployeeRegistration, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return domain.EmployeeRegistration{}, err
	}
	if payload == "" {
		payload = "{}"
	}
	var item domain.EmployeeRegistration
	err = r.db.QueryRowContext(ctx, `
		UPDATE hrm_employee_registrations
		SET payload = $3::jsonb, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft'
		RETURNING id, tenant_id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at`,
		tenant, id, payload,
	).Scan(&item.ID, &item.TenantID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) SubmitEmployeeRegistration(ctx context.Context, id, workflowCaseID string) (domain.EmployeeRegistration, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return domain.EmployeeRegistration{}, err
	}
	var caseID *string
	if workflowCaseID != "" {
		caseID = &workflowCaseID
	}
	var item domain.EmployeeRegistration
	err = r.db.QueryRowContext(ctx, `
		UPDATE hrm_employee_registrations
		SET status = 'submitted', workflow_case_id = COALESCE($3, workflow_case_id), updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, registration_code, payload::text, workflow_case_id, status, created_by, created_at, updated_at`,
		tenant, id, caseID,
	).Scan(&item.ID, &item.TenantID, &item.RegistrationCode, &item.Payload, &item.WorkflowCaseID, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}
