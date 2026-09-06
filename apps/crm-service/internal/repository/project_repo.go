package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	)

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

func (r *ProjectRepository) UpsertType(ctx context.Context, t *ProjectType) (*ProjectType, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO crm_project_types (id, tenant_id, code, name, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, is_active = EXCLUDED.is_active,
			updated_at = now(), version = crm_project_types.version + 1
		RETURNING created_at, updated_at`,
		t.ID, t.TenantID, t.Code, t.Name, t.IsActive, t.CreatedBy)
	if err := row.Scan(&t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *ProjectRepository) ListProjects(ctx context.Context, tenantID string, orgCodes []string, status, q string) ([]Project, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, project_code, name, type_code, customer_id::text, parent_code::text,
		       start_date::text, end_date::text, status, description::text, COALESCE(org_code,''),
		       created_by, created_at, updated_at
		FROM crm_projects
		WHERE tenant_id = $1
		  AND ($4::text = '' OR status = $4::text)
		  AND ($5::text = '' OR project_code ILIKE '%'||$5::text||'%' OR name ILIKE '%'||$5::text||'%')
		  AND ($6::text[] IS NULL OR org_code = ANY($6::text[]))
		ORDER BY project_code LIMIT 200`, tenantID, orgCodes, status, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		var customerID, parentCode, startDate, endDate, description sql.NullString
		if err := rows.Scan(&p.ID, &p.TenantID, &p.ProjectCode, &p.Name, &p.TypeCode, &customerID,
			&parentCode, &startDate, &endDate, &p.Status, &description, &p.OrgCode,
			&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
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
	return out, rows.Err()
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

func (r *ProjectRepository) ListRiskFlags(ctx context.Context, tenantID, customerID string) ([]RiskFlag, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, customer_id, flag_code, severity, note::text, effective_date::text,
		       expiry_date::text, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM crm_customer_risk_flags
		WHERE tenant_id = $1 AND customer_id = $2 AND is_active
		ORDER BY effective_date DESC`, tenantID, customerID)
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

func (r *ProjectRepository) AddRiskFlag(ctx context.Context, f *RiskFlag) (*RiskFlag, error) {
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

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

var _ = time.Now
var _ = fmt.Sprintf
