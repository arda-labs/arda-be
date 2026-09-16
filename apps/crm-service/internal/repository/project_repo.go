package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	ardapg "github.com/arda-labs/arda/libs/go/arda-postgres"
)

// ErrNotFound marks a scoped row (project, member, customer parent) that is
// missing from the caller's tenant+org scope. It wraps sql.ErrNoRows so the
// shared ardahttp.WriteServiceError mapper renders it as a 404 problem.
var ErrNotFound = fmt.Errorf("crm: resource not found: %w", sql.ErrNoRows)

// scopedNotFound wraps ErrNotFound with the resource that failed the scoped
// lookup; the extra context is for logs only (the problem mapper omits it).
func scopedNotFound(resource, id string) error {
	return fmt.Errorf("%w: %s %s", ErrNotFound, resource, id)
}

// ProjectType is a project classification catalog row.
type ProjectType struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Project is a credit/infrastructure project (EPAS inf_project).
type Project struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	ProjectCode string    `json:"project_code"`
	Name        string    `json:"name"`
	TypeCode    string    `json:"type_code"`
	CustomerID  *string   `json:"customer_id,omitempty"`
	ParentCode  *string   `json:"parent_code,omitempty"`
	StartDate   *string   `json:"start_date,omitempty"`
	EndDate     *string   `json:"end_date,omitempty"`
	Status      string    `json:"status"`
	Description *string   `json:"description,omitempty"`
	OrgCode     string    `json:"org_code,omitempty"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectMember is one user assigned to a project.
type ProjectMember struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	ProjectID string    `json:"project_id"`
	UserID    string    `json:"user_id"`
	RoleCode  *string   `json:"role_code,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// RiskFlag is a customer risk marker (EPAS customer/risk-info).
type RiskFlag struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	CustomerID    string    `json:"customer_id"`
	FlagCode      string    `json:"flag_code"`
	Severity      string    `json:"severity"`
	Note          *string   `json:"note,omitempty"`
	EffectiveDate string    `json:"effective_date"`
	ExpiryDate    *string   `json:"expiry_date,omitempty"`
	IsActive      bool      `json:"is_active"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ProjectRepository persists projects, project types and risk flags.
type ProjectRepository struct {
	db *sql.DB
}

func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

// ListTypes returns active project types.
func (r *ProjectRepository) ListTypes(ctx context.Context, tenantID string) ([]ProjectType, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM crm_project_types WHERE tenant_id IN ('', $1) AND is_active ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectType{}
	for rows.Next() {
		var t ProjectType
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Code, &t.Name, &t.IsActive, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpsertType creates or updates one project type by (tenant_id, code). The id
// is always server-generated and any body-supplied id is ignored, so a caller
// cannot address (and therefore overwrite) another tenant's row; conflict
// resolution targets the real unique key of crm_project_types.
func (r *ProjectRepository) UpsertType(ctx context.Context, t *ProjectType) (*ProjectType, error) {
	t.ID = ""
	id, err := newID()
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO crm_project_types (id, tenant_id, code, name, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name, is_active = EXCLUDED.is_active,
			updated_at = now(), version = crm_project_types.version + 1
		WHERE crm_project_types.tenant_id = EXCLUDED.tenant_id
		RETURNING id, created_at, updated_at`,
		id, t.TenantID, t.Code, t.Name, t.IsActive, t.CreatedBy)
	if err := row.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

// projectListMaxPerPage caps one page of projects (mirrors
// ardahttp.MaxPerPage).
const projectListMaxPerPage = 100

// projectListWhere builds the shared WHERE clause for the count and page
// queries. $1 is always the tenant scope, so both stay tenant-bound.
func projectListWhere(tenantID string, orgCodes []string, status, q string) (string, []any) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(project_code ILIKE $%d OR name ILIKE $%d)", len(args), len(args)))
	}
	if len(orgCodes) > 0 {
		args = append(args, ardapg.Driver.NotNil(orgCodes))
		where = append(where, fmt.Sprintf("org_code = ANY($%d)", len(args)))
	}
	return strings.Join(where, " AND "), args
}

// projectListBounds clamps page/per_page so OFFSET can never go negative.
func projectListBounds(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage <= 0 || perPage > projectListMaxPerPage {
		perPage = projectListMaxPerPage
	}
	return page, perPage
}

// projectScopeWhere builds the tenant + optional org predicate shared by every
// single-project statement (get/update/member queries) so they cannot drift
// from the listing scope. offset is the number of placeholders the caller has
// already reserved, and the returned args append cleanly after them. An empty
// org scope (global/superadmin caller) keeps tenant-only filtering, mirroring
// ListProjects.
func projectScopeWhere(tenantID, projectID string, orgCodes []string, offset int) (string, []any) {
	clauses := []string{
		fmt.Sprintf("tenant_id = $%d", offset+1),
		fmt.Sprintf("id = $%d::uuid", offset+2),
	}
	args := []any{tenantID, projectID}
	if len(orgCodes) > 0 {
		args = append(args, ardapg.Driver.NotNil(orgCodes))
		clauses = append(clauses, fmt.Sprintf("org_code = ANY($%d)", offset+len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

// customerScopeWhere is the same predicate for one customer parent row, used
// before inserting child rows (risk flags). It mirrors projectScopeWhere but
// customers.id is VARCHAR (no uuid cast) and the org column is org_id.
func customerScopeWhere(tenantID, customerID string, orgCodes []string, offset int) (string, []any) {
	clauses := []string{
		fmt.Sprintf("tenant_id = $%d", offset+1),
		fmt.Sprintf("id = $%d", offset+2),
	}
	args := []any{tenantID, customerID}
	if len(orgCodes) > 0 {
		args = append(args, ardapg.Driver.NotNil(orgCodes))
		clauses = append(clauses, fmt.Sprintf("org_id = ANY($%d)", offset+len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

// projectInScope reports whether the project exists in the caller's tenant+org
// scope. Child rows (members) must never be attached by guessing a project id
// from another scope.
func (r *ProjectRepository) projectInScope(ctx context.Context, tenantID, projectID string, orgCodes []string) (bool, error) {
	where, args := projectScopeWhere(tenantID, projectID, orgCodes, 0)
	var exists bool
	err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM crm_projects WHERE "+where+")", args...).Scan(&exists)
	return exists, err
}

// customerInScope is the parent check for risk flags: the customer must be in
// the caller's tenant+org scope before a flag can be attached.
func (r *ProjectRepository) customerInScope(ctx context.Context, tenantID, customerID string, orgCodes []string) (bool, error) {
	where, args := customerScopeWhere(tenantID, customerID, orgCodes, 0)
	var exists bool
	err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM customers WHERE "+where+")", args...).Scan(&exists)
	return exists, err
}

// ListProjects returns one page of projects plus the total number of rows
// matching the same filters, so tenants with more than one page of projects
// are not silently truncated.
func (r *ProjectRepository) ListProjects(ctx context.Context, tenantID string, orgCodes []string, status, q string, page, perPage int) ([]Project, int, error) {
	page, perPage = projectListBounds(page, perPage)
	whereSQL, args := projectListWhere(tenantID, orgCodes, status, q)

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM crm_projects WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, project_code, name, type_code, customer_id::text, parent_code::text,
		       start_date::text, end_date::text, status, description::text, COALESCE(org_code,''),
		       created_by, created_at, updated_at
		FROM crm_projects
		WHERE %s
		ORDER BY project_code
		LIMIT $%d OFFSET $%d`, whereSQL, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		var customerID, parentCode, startDate, endDate, description sql.NullString
		if err := rows.Scan(&p.ID, &p.TenantID, &p.ProjectCode, &p.Name, &p.TypeCode, &customerID,
			&parentCode, &startDate, &endDate, &p.Status, &description, &p.OrgCode,
			&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, err
		}
		p.CustomerID = nullStringPtr(customerID)
		p.ParentCode = nullStringPtr(parentCode)
		if startDate.Valid {
			p.StartDate = &startDate.String
		}
		if endDate.Valid {
			p.EndDate = &endDate.String
		}
		p.Description = nullStringPtr(description)
		out = append(out, p)
	}
	return out, total, rows.Err()
}

func (r *ProjectRepository) CreateProject(ctx context.Context, p *Project) (*Project, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO crm_projects (tenant_id, project_code, name, type_code, customer_id, parent_code,
			start_date, end_date, status, description, org_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7::date,$8::date,$9,$10,$11,$12)
		RETURNING id, created_at, updated_at`,
		p.TenantID, p.ProjectCode, p.Name, p.TypeCode, nullUUIDPtr(p.CustomerID), nullUUIDPtr(p.ParentCode),
		p.StartDate, p.EndDate, p.Status, p.Description, nullString(p.OrgCode), p.CreatedBy)
	if err := row.Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ListRiskFlags returns active risk flags of one customer, only when the
// customer itself is visible in the caller's tenant+org scope.
func (r *ProjectRepository) ListRiskFlags(ctx context.Context, tenantID, customerID string, orgCodes []string) ([]RiskFlag, error) {
	customerWhere, args := customerScopeWhere(tenantID, customerID, orgCodes, 0)
	rows, err := r.db.QueryContext(ctx, `
		SELECT f.id, f.tenant_id, f.customer_id, f.flag_code, f.severity, f.note::text, f.effective_date::text,
		       f.expiry_date::text, f.is_active, COALESCE(f.created_by,''), f.created_at, f.updated_at
		FROM crm_customer_risk_flags f
		WHERE f.tenant_id = $1 AND f.customer_id = $2 AND f.is_active
		  AND EXISTS (SELECT 1 FROM customers WHERE `+customerWhere+`)
		ORDER BY f.effective_date DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RiskFlag{}
	for rows.Next() {
		var f RiskFlag
		var note, expiry sql.NullString
		if err := rows.Scan(&f.ID, &f.TenantID, &f.CustomerID, &f.FlagCode, &f.Severity, &note,
			&f.EffectiveDate, &expiry, &f.IsActive, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.Note = nullStringPtr(note)
		f.ExpiryDate = nullStringPtr(expiry)
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddRiskFlag inserts one customer risk flag after verifying the parent
// customer exists in the caller's tenant+org scope; a foreign or missing
// customer yields ErrNotFound instead of a cross-tenant child row.
func (r *ProjectRepository) AddRiskFlag(ctx context.Context, f *RiskFlag, orgCodes []string) (*RiskFlag, error) {
	exists, err := r.customerInScope(ctx, f.TenantID, f.CustomerID, orgCodes)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, scopedNotFound("customer", f.CustomerID)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO crm_customer_risk_flags
			(tenant_id, customer_id, flag_code, severity, note, effective_date, expiry_date, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6::date,$7::date,$8,$9)
		RETURNING id, created_at, updated_at`,
		f.TenantID, f.CustomerID, f.FlagCode, f.Severity, f.Note, f.EffectiveDate, f.ExpiryDate,
		f.IsActive, f.CreatedBy)
	if err := row.Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	return f, nil
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullUUIDPtr(s *string) any {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return *s
}

// GetProject loads one project by id within the caller's tenant+org scope.
func (r *ProjectRepository) GetProject(ctx context.Context, tenantID, id string, orgCodes []string) (*Project, error) {
	where, args := projectScopeWhere(tenantID, id, orgCodes, 0)
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, project_code, name, type_code, customer_id::text, parent_code::text,
		       start_date::text, end_date::text, status, description::text, COALESCE(org_code,''),
		       created_by, created_at, updated_at
		FROM crm_projects WHERE `+where, args...)
	var p Project
	err := row.Scan(&p.ID, &p.TenantID, &p.ProjectCode, &p.Name, &p.TypeCode, &p.CustomerID, &p.ParentCode,
		&p.StartDate, &p.EndDate, &p.Status, &p.Description, &p.OrgCode, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// projectUpdateStatement renders the project update guarded by the caller's
// tenant+org predicate. SET placeholders are always $1..$7, so the predicate
// must start at $8 (see projectScopeWhere offset) and can never silently drop
// the scope filter.
func projectUpdateStatement(where string) string {
	return `UPDATE crm_projects SET name = $1, type_code = $2, status = $3,
			start_date = NULLIF($4,'')::date, end_date = NULLIF($5,'')::date,
			description = NULLIF($6,''), updated_by = $7, updated_at = now(), version = version + 1
		WHERE ` + where + `
		RETURNING project_code, customer_id::text, COALESCE(org_code,''), created_by, created_at, updated_at`
}

// UpdateProject updates the editable fields of one project inside the caller's
// tenant+org scope; a missing or foreign project surfaces as ErrNotFound.
func (r *ProjectRepository) UpdateProject(ctx context.Context, tenantID, id, actor string, orgCodes []string, p *Project) (*Project, error) {
	args := []any{p.Name, p.TypeCode, p.Status, derefOr(p.StartDate), derefOr(p.EndDate),
		derefOr(p.Description), actor}
	where, scopeArgs := projectScopeWhere(tenantID, id, orgCodes, len(args))
	args = append(args, scopeArgs...)
	row := r.db.QueryRowContext(ctx, projectUpdateStatement(where), args...)
	if err := row.Scan(&p.ProjectCode, &p.CustomerID, &p.OrgCode, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, scopedNotFound("project", id)
		}
		return nil, err
	}
	p.ID = id
	p.TenantID = tenantID
	return p, nil
}

// ListProjectMembers returns active members of one project, only when the
// project itself is visible in the caller's tenant+org scope.
func (r *ProjectRepository) ListProjectMembers(ctx context.Context, tenantID, projectID string, orgCodes []string) ([]ProjectMember, error) {
	projectWhere, args := projectScopeWhere(tenantID, projectID, orgCodes, 0)
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id::text, m.tenant_id, m.project_id::text, m.user_id, m.role_code, m.is_active,
		       COALESCE(m.created_by,''), m.created_at
		FROM crm_project_members m
		WHERE m.tenant_id = $1 AND m.project_id = $2::uuid AND m.is_active
		  AND EXISTS (SELECT 1 FROM crm_projects WHERE `+projectWhere+`)
		ORDER BY m.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectMember{}
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.ID, &m.TenantID, &m.ProjectID, &m.UserID, &m.RoleCode, &m.IsActive,
			&m.CreatedBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddProjectMember upserts one project member by (tenant, project, user) after
// verifying the parent project exists in the caller's tenant+org scope; a
// foreign or missing project yields ErrNotFound instead of a cross-tenant
// child row.
func (r *ProjectRepository) AddProjectMember(ctx context.Context, m *ProjectMember, orgCodes []string) (*ProjectMember, error) {
	exists, err := r.projectInScope(ctx, m.TenantID, m.ProjectID, orgCodes)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, scopedNotFound("project", m.ProjectID)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO crm_project_members (tenant_id, project_id, user_id, role_code, is_active, created_by)
		VALUES ($1,$2::uuid,$3,NULLIF($4,''),true,$5)
		ON CONFLICT (tenant_id, project_id, user_id) DO UPDATE SET
			role_code = EXCLUDED.role_code, is_active = true, created_at = now()
		RETURNING id::text, created_at`,
		m.TenantID, m.ProjectID, m.UserID, derefOr(m.RoleCode), m.CreatedBy)
	if err := row.Scan(&m.ID, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.IsActive = true
	return m, nil
}

// RemoveProjectMember soft-deletes one project member inside the caller's
// tenant+org scope; a missing or foreign row surfaces as ErrNotFound.
func (r *ProjectRepository) RemoveProjectMember(ctx context.Context, tenantID, projectID, memberID string, orgCodes []string) error {
	args := []any{tenantID, projectID, memberID}
	projectWhere, scopeArgs := projectScopeWhere(tenantID, projectID, orgCodes, len(args))
	args = append(args, scopeArgs...)
	res, err := r.db.ExecContext(ctx, `
		UPDATE crm_project_members SET is_active = false
		WHERE tenant_id = $1 AND project_id = $2::uuid AND id = $3::uuid
		  AND EXISTS (SELECT 1 FROM crm_projects WHERE `+projectWhere+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return scopedNotFound("project member", memberID)
	}
	return nil
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
