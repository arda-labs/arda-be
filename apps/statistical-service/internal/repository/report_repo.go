package repository

import (
	"context"
	cryptoRandStat "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"
)

// ReportDefinition is one report catalog row (Q8: query param-hoá, template
// Excel ở media-service).
type ReportDefinition struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	Code           string          `json:"code"`
	Name           string          `json:"name"`
	GroupCode      string          `json:"group_code,omitempty"`
	QueryID        string          `json:"query_id"`
	ParamSchema    json.RawMessage `json:"param_schema"`
	TemplateFileID *string         `json:"template_file_id,omitempty"`
	OutputFormat   string          `json:"output_format"`
	IsActive       bool            `json:"is_active"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Indicator is one statistical indicator catalog row.
type Indicator struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Unit      string    `json:"unit,omitempty"`
	GroupCode string    `json:"group_code,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReportSubmission is one submitted report period (maker-checker qua case).
type ReportSubmission struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	ReportCode     string          `json:"report_code"`
	PeriodCode     string          `json:"period_code"`
	Status         string          `json:"status"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	WorkflowCaseID *string         `json:"workflow_case_id,omitempty"`
	SubmittedBy    string          `json:"submitted_by,omitempty"`
	SubmittedAt    *time.Time      `json:"submitted_at,omitempty"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// StatisticalRepository persists report definitions + indicators + submissions.
type StatisticalRepository struct {
	db *sql.DB
}

func NewStatisticalRepository(db *sql.DB) *StatisticalRepository {
	return &StatisticalRepository{db: db}
}

// ListReportDefinitions returns active report definitions.
func (r *StatisticalRepository) ListReportDefinitions(ctx context.Context, tenantID string) ([]ReportDefinition, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, COALESCE(group_code,''), query_id, param_schema,
		       template_file_id::text, output_format, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM rpt_report_definitions WHERE tenant_id = $1 AND is_active ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportDefinition{}
	for rows.Next() {
		var d ReportDefinition
		var template, group sql.NullString
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Code, &d.Name, &group, &d.QueryID, &d.ParamSchema,
			&template, &d.OutputFormat, &d.IsActive, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.GroupCode = group.String
		if template.Valid {
			d.TemplateFileID = &template.String
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpsertReportDefinition creates or updates one definition.
func (r *StatisticalRepository) UpsertReportDefinition(ctx context.Context, d *ReportDefinition) (*ReportDefinition, error) {
	if d.ID == "" {
		d.ID = NewStatisticalID("rptdef")
	}
	if d.ParamSchema == nil {
		d.ParamSchema = []byte("{}")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_report_definitions (id, tenant_id, code, name, group_code, query_id, param_schema, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name, group_code = EXCLUDED.group_code,
			query_id = EXCLUDED.query_id, param_schema = EXCLUDED.param_schema,
			updated_at = now(), version = rpt_report_definitions.version + 1
		RETURNING created_at, updated_at`,
		d.ID, d.TenantID, d.Code, d.Name, nullStringStat(d.GroupCode), d.QueryID, d.ParamSchema, d.CreatedBy)
	if err := row.Scan(&d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return d, nil
}

// ListIndicators returns indicator catalog rows.
func (r *StatisticalRepository) ListIndicators(ctx context.Context, tenantID string) ([]Indicator, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, COALESCE(unit,''), COALESCE(group_code,''), is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM rpt_indicators WHERE tenant_id = $1 AND is_active ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Indicator{}
	for rows.Next() {
		var i Indicator
		if err := rows.Scan(&i.ID, &i.TenantID, &i.Code, &i.Name, &i.Unit, &i.GroupCode,
			&i.IsActive, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UpsertIndicator creates or updates one indicator.
func (r *StatisticalRepository) UpsertIndicator(ctx context.Context, i *Indicator) (*Indicator, error) {
	if i.ID == "" {
		i.ID = NewStatisticalID("ind")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_indicators (id, tenant_id, code, name, unit, group_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name, unit = EXCLUDED.unit,
			group_code = EXCLUDED.group_code, updated_at = now(), version = rpt_indicators.version + 1
		RETURNING created_at, updated_at`,
		i.ID, i.TenantID, i.Code, i.Name, nullStringStat(i.Unit), nullStringStat(i.GroupCode), i.CreatedBy)
	if err := row.Scan(&i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	return i, nil
}

// CreateSubmission records a report submission.
func (r *StatisticalRepository) CreateSubmission(ctx context.Context, sub *ReportSubmission) (*ReportSubmission, error) {
	if sub.ID == "" {
		sub.ID = NewStatisticalID("rptsub")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_report_submissions (id, tenant_id, report_code, period_code, status, payload, submitted_by, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING created_at`,
		sub.ID, sub.TenantID, sub.ReportCode, sub.PeriodCode, sub.Status, sub.Payload,
		nullStringStat(sub.SubmittedBy), sub.CreatedBy)
	if err := row.Scan(&sub.CreatedAt); err != nil {
		return nil, err
	}
	return sub, nil
}

// ListSubmissions returns submissions filtered by report/period/status.
func (r *StatisticalRepository) ListSubmissions(ctx context.Context, tenantID, reportCode, periodCode, status string) ([]ReportSubmission, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, report_code, period_code, status, payload,
		       workflow_case_id::text, COALESCE(submitted_by,''), submitted_at, COALESCE(created_by,''), created_at, updated_at
		FROM rpt_report_submissions
		WHERE tenant_id = $1
		  AND ($2::text = '' OR report_code = $2::text)
		  AND ($3::text = '' OR period_code = $3::text)
		  AND ($4::text = '' OR status = $4::text)
		ORDER BY created_at DESC LIMIT 200`, tenantID, reportCode, periodCode, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportSubmission{}
	for rows.Next() {
		var sub ReportSubmission
		var caseID sql.NullString
		var submittedAt sql.NullTime
		if err := rows.Scan(&sub.ID, &sub.TenantID, &sub.ReportCode, &sub.PeriodCode, &sub.Status,
			&sub.Payload, &caseID, &sub.SubmittedBy, &submittedAt, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		if caseID.Valid {
			sub.WorkflowCaseID = &caseID.String
		}
		if submittedAt.Valid {
			sub.SubmittedAt = &submittedAt.Time
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// NewStatisticalID generates a prefixed random id.
func NewStatisticalID(prefix string) string {
	var b [16]byte
	if _, err := cryptoRandStat.Read(b[:]); err != nil {
		panic("statistical id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func nullStringStat(s string) any {
	if s == "" {
		return nil
	}
	return s
}
