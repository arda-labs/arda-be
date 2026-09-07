package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/hrm-service/internal/domain"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// listSortDirection maps the FE order param to a SQL direction.
func listSortDirection(order string) string {
	if strings.EqualFold(order, "desc") {
		return "DESC"
	}
	return "ASC"
}

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

// ListPositionsParams is the parsed list contract for hrm_positions. All is
// set for lookup consumers (all=1) which receive the full result set.
type ListPositionsParams struct {
	Status  string
	Q       string
	Sort    string
	Order   string
	Page    int
	PerPage int
	All     bool
}

func (r *HRMRepository) ListPositions(ctx context.Context, params ListPositionsParams) ([]domain.Position, int, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, 0, err
	}
	where := []string{"tenant_id = $1"}
	args := []any{tenant}
	idx := 2
	if params.Status != "" {
		where = append(where, fmt.Sprintf("status = ANY(string_to_array($%d, ','))", idx))
		args = append(args, params.Status)
		idx++
	}
	if params.Q != "" {
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Q+"%")
		idx++
	}
	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hrm_positions WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count positions: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, code, name, status, is_manager, description, created_at, updated_at
		FROM hrm_positions
		WHERE %s
		ORDER BY %s %s`, wc, positionSortCol(params.Sort), listSortDirection(params.Order))
	if !params.All {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", idx, idx+1)
		args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.Position, 0)
	for rows.Next() {
		var item domain.Position
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Status, &item.IsManager, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// positionSortCol maps the FE sort param to a whitelisted column; unknown or
// empty sorts fall back to the legacy default order (code).
func positionSortCol(sort string) string {
	switch sort {
	case "code":
		return "code"
	case "name":
		return "name"
	case "status":
		return "status"
	case "created_at":
		return "created_at"
	default:
		return "code"
	}
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

// ListJobTitlesParams is the parsed list contract for hrm_job_titles.
type ListJobTitlesParams struct {
	Q       string
	Sort    string
	Order   string
	Page    int
	PerPage int
	All     bool
}

func (r *HRMRepository) ListJobTitles(ctx context.Context, params ListJobTitlesParams) ([]domain.JobTitle, int, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, 0, err
	}
	where := []string{"tenant_id = $1"}
	args := []any{tenant}
	idx := 2
	if params.Q != "" {
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Q+"%")
		idx++
	}
	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hrm_job_titles WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count job titles: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, code, name, description, created_at, updated_at
		FROM hrm_job_titles
		WHERE %s
		ORDER BY %s %s`, wc, jobTitleSortCol(params.Sort), listSortDirection(params.Order))
	if !params.All {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", idx, idx+1)
		args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.JobTitle, 0)
	for rows.Next() {
		var item domain.JobTitle
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// jobTitleSortCol maps the FE sort param to a whitelisted column; unknown or
// empty sorts fall back to the legacy default order (code).
func jobTitleSortCol(sort string) string {
	switch sort {
	case "code":
		return "code"
	case "name":
		return "name"
	case "created_at":
		return "created_at"
	default:
		return "code"
	}
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

// ListOrgUnitsParams is the parsed list contract for hrm_org_units. All is
// set for tree/lookup consumers (all=1, view=tree) which receive the full
// result set.
type ListOrgUnitsParams struct {
	OrganizationID string
	Status         string
	Q              string
	Sort           string
	Order          string
	Page           int
	PerPage        int
	All            bool
}

func (r *HRMRepository) ListOrgUnits(ctx context.Context, params ListOrgUnitsParams) ([]domain.OrgUnit, int, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, 0, err
	}
	where := []string{"tenant_id = $1"}
	args := []any{tenant}
	idx := 2
	if params.OrganizationID != "" {
		where = append(where, fmt.Sprintf("organization_id = $%d", idx))
		args = append(args, params.OrganizationID)
		idx++
	}
	if params.Status != "" {
		where = append(where, fmt.Sprintf("status = ANY(string_to_array($%d, ','))", idx))
		args = append(args, params.Status)
		idx++
	}
	if params.Q != "" {
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Q+"%")
		idx++
	}
	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hrm_org_units WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count org units: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, code, organization_id, name, org_level, parent_id, department_type, status, description, created_at, updated_at
		FROM hrm_org_units
		WHERE %s
		ORDER BY %s`, wc, orgUnitOrderBy(params.Sort, params.Order))
	if !params.All {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", idx, idx+1)
		args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.OrgUnit, 0)
	for rows.Next() {
		var item domain.OrgUnit
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.OrganizationID, &item.Name, &item.OrgLevel, &item.ParentID, &item.DepartmentType, &item.Status, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// orgUnitOrderBy maps the FE sort param to a whitelisted ORDER BY expression;
// unknown or empty sorts fall back to the legacy parent-first tree order so
// tree/lookup consumers keep receiving parents before children.
func orgUnitOrderBy(sort, order string) string {
	direction := listSortDirection(order)
	switch sort {
	case "code":
		return "code " + direction
	case "name":
		return "name " + direction
	case "status":
		return "status " + direction
	case "org_level":
		return "org_level " + direction
	case "created_at":
		return "created_at " + direction
	default:
		return "parent_id NULLS FIRST, code"
	}
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

// ListEmployeesParams is the parsed list contract for hrm_employees.
type ListEmployeesParams struct {
	Status  string
	Q       string
	Sort    string
	Order   string
	Page    int
	PerPage int
	All     bool
}

func (r *HRMRepository) ListEmployees(ctx context.Context, params ListEmployeesParams) ([]domain.Employee, int, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, 0, err
	}
	where := []string{"tenant_id = $1"}
	args := []any{tenant}
	idx := 2
	if params.Status != "" {
		where = append(where, fmt.Sprintf("status = ANY(string_to_array($%d, ','))", idx))
		args = append(args, params.Status)
		idx++
	}
	if params.Q != "" {
		where = append(where, fmt.Sprintf("(employee_code ILIKE $%d OR full_name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Q+"%")
		idx++
	}
	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hrm_employees WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count employees: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, employee_code, full_name, org_unit_id, position_id, job_title_id, iam_user_id, status, created_at, updated_at
		FROM hrm_employees
		WHERE %s
		ORDER BY %s %s`, wc, employeeSortCol(params.Sort), listSortDirection(params.Order))
	if !params.All {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", idx, idx+1)
		args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.Employee, 0)
	for rows.Next() {
		var item domain.Employee
		if err := rows.Scan(&item.ID, &item.TenantID, &item.EmployeeCode, &item.FullName, &item.OrgUnitID, &item.PositionID, &item.JobTitleID, &item.IAMUserID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// employeeSortCol maps the FE sort param to a whitelisted column; unknown or
// empty sorts fall back to the legacy default order (employee_code).
func employeeSortCol(sort string) string {
	switch sort {
	case "employee_code":
		return "employee_code"
	case "full_name":
		return "full_name"
	case "created_at":
		return "created_at"
	default:
		return "employee_code"
	}
}

func (r *HRMRepository) CreateEmployee(ctx context.Context, item domain.Employee) (domain.Employee, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	if item.ID == "" {
		item.ID = newID("emp")
	}
	item.Status = active(item.Status)
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_employees (id, tenant_id, employee_code, full_name, org_unit_id, position_id, job_title_id, iam_user_id, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, employee_code, full_name, org_unit_id, position_id, job_title_id, iam_user_id, status, created_at, updated_at`,
		item.ID, item.TenantID, item.EmployeeCode, item.FullName, item.OrgUnitID, item.PositionID, item.JobTitleID, item.IAMUserID, item.Status,
	).Scan(&item.ID, &item.TenantID, &item.EmployeeCode, &item.FullName, &item.OrgUnitID, &item.PositionID, &item.JobTitleID, &item.IAMUserID, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdateEmployee(ctx context.Context, item domain.Employee) (domain.Employee, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	item.Status = active(item.Status)
	err = r.db.QueryRowContext(ctx, `
		UPDATE hrm_employees
		SET employee_code = $3, full_name = $4, org_unit_id = $5, position_id = $6, job_title_id = $7,
			iam_user_id = $8, status = $9, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, employee_code, full_name, org_unit_id, position_id, job_title_id, iam_user_id, status, created_at, updated_at`,
		tenant, item.ID, item.EmployeeCode, item.FullName, item.OrgUnitID, item.PositionID, item.JobTitleID, item.IAMUserID, item.Status,
	).Scan(&item.ID, &item.TenantID, &item.EmployeeCode, &item.FullName, &item.OrgUnitID, &item.PositionID, &item.JobTitleID, &item.IAMUserID, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeleteEmployee(ctx context.Context, id string) error {
	tenant, err := tenantID(ctx)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM hrm_employees WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return err
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

func (r *HRMRepository) ListEmployeeStatuses(ctx context.Context, q string) ([]domain.EmployeeStatus, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, description, created_at, updated_at
		FROM hrm_employee_statuses
		WHERE tenant_id = $1 AND ($2 = '' OR code ILIKE '%' || $2 || '%' OR name ILIKE '%' || $2 || '%')
		ORDER BY code`, tenant, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.EmployeeStatus, 0)
	for rows.Next() {
		var item domain.EmployeeStatus
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *HRMRepository) CreateEmployeeStatus(ctx context.Context, item domain.EmployeeStatus) (domain.EmployeeStatus, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	if item.ID == "" {
		item.ID = newID("estat")
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO hrm_employee_statuses (id, tenant_id, code, name, description)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, code, name, description, created_at, updated_at`,
		item.ID, item.TenantID, item.Code, item.Name, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) UpdateEmployeeStatus(ctx context.Context, item domain.EmployeeStatus) (domain.EmployeeStatus, error) {
	tenant, err := tenantID(ctx)
	if err != nil {
		return item, err
	}
	item.TenantID = tenant
	err = r.db.QueryRowContext(ctx, `
		UPDATE hrm_employee_statuses
		SET code = $3, name = $4, description = $5, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, code, name, description, created_at, updated_at`,
		tenant, item.ID, item.Code, item.Name, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *HRMRepository) DeleteEmployeeStatus(ctx context.Context, id string) error {
	tenant, err := tenantID(ctx)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM hrm_employee_statuses WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return err
}
