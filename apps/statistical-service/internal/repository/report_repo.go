package repository

import (
	"context"
	cryptoRandStat "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	UpdatedBy      string          `json:"updated_by,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Indicator is one statistical indicator catalog row. The first six fields are
// the original catalog shape; the rest are the QCMS KPI model added by
// 20260922120000_rpt_indicator_model.sql (see arda-be/docs/reporting-data-layer.md
// §5.3). `Formula` is declarative JSON (members reference indicators/facts and
// columns) — never SQL text.
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

	// QCMS KPI model.
	KpiType        string          `json:"kpi_type"`    // P | C
	Periodicity    string          `json:"periodicity"` // D|W|M|Q|Y
	IsRatio        bool            `json:"is_ratio"`
	RootCode       string          `json:"root_code,omitempty"`
	RootName       string          `json:"root_name,omitempty"`
	Meaning        string          `json:"meaning,omitempty"`
	Sources        json.RawMessage `json:"sources"`
	Formula        json.RawMessage `json:"formula"`
	Dimensions     json.RawMessage `json:"dimensions"`
	DisplayFormat  string          `json:"display_format,omitempty"`
	RoundingDigits int             `json:"rounding_digits"`
	ScoreGroup     string          `json:"score_group,omitempty"`
	Weight         *float64        `json:"weight,omitempty"`
}

// IndicatorResult is one computed/manual value for an indicator in a period.
type IndicatorResult struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	IndicatorCode string    `json:"indicator_code"`
	PeriodCode    string    `json:"period_code"`
	DimensionKey  string    `json:"dimension_key"`
	BusinessDate  string    `json:"business_date,omitempty"`
	Value         *float64  `json:"value,omitempty"`
	Revision      int       `json:"revision"`
	Source        string    `json:"source"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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
	// DataVersion is the row version the checker saw (rpt_report_submissions.version).
	DataVersion int64 `json:"data_version"`
}

// StatisticalRepository persists report definitions + indicators + submissions.
type StatisticalRepository struct {
	db *sql.DB
}

func NewStatisticalRepository(db *sql.DB) *StatisticalRepository {
	return &StatisticalRepository{db: db}
}

// ListReportDefinitionsParams carries the parsed list query for report
// definitions (same small-catalog unpaged contract as indicators).
type ListReportDefinitionsParams struct {
	TenantID string
	Q        string
	Sort     string
	Order    string
}

// reportDefinitionSortCol maps the FE sort param to a whitelisted column.
func reportDefinitionSortCol(sort string) string {
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

// ListReportDefinitions returns active report definitions filtered by q,
// sorted by the whitelisted column.
func (r *StatisticalRepository) ListReportDefinitions(ctx context.Context, params ListReportDefinitionsParams) ([]ReportDefinition, error) {
	where := "tenant_id = $1 AND is_active"
	args := []any{params.TenantID}
	if params.Q != "" {
		args = append(args, "%"+params.Q+"%")
		where = fmt.Sprintf(
			"tenant_id = $1 AND is_active AND (code ILIKE $%d OR name ILIKE $%d)",
			len(args), len(args))
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, code, name, COALESCE(group_code,''), query_id, param_schema,
		       template_file_id::text, output_format, is_active, COALESCE(created_by,''),
		       COALESCE(updated_by,''), created_at, updated_at
		FROM rpt_report_definitions WHERE %s ORDER BY %s %s`,
		where, reportDefinitionSortCol(params.Sort), listStatOrder(params.Order)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportDefinition{}
	for rows.Next() {
		var d ReportDefinition
		var template, group sql.NullString
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Code, &d.Name, &group, &d.QueryID, &d.ParamSchema,
			&template, &d.OutputFormat, &d.IsActive, &d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
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
	if d.OutputFormat == "" {
		d.OutputFormat = "XLSX"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_report_definitions (id, tenant_id, code, name, group_code, query_id, param_schema,
			template_file_id, output_format, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name, group_code = EXCLUDED.group_code,
			query_id = EXCLUDED.query_id, param_schema = EXCLUDED.param_schema,
			template_file_id = EXCLUDED.template_file_id, output_format = EXCLUDED.output_format,
			updated_by = EXCLUDED.updated_by,
			updated_at = now(), version = rpt_report_definitions.version + 1
		RETURNING created_at, updated_at`,
		d.ID, d.TenantID, d.Code, d.Name, nullStringStat(d.GroupCode), d.QueryID, d.ParamSchema,
		d.TemplateFileID, d.OutputFormat, d.CreatedBy, nullStringStat(d.UpdatedBy))
	if err := row.Scan(&d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return d, nil
}

// ListIndicatorsParams carries the parsed list query for indicators.
// Both catalog tables are small, so the list stays unpaged; q narrows the set
// and sort is a repo-whitelisted column so export and table stay consistent.
type ListIndicatorsParams struct {
	TenantID string
	Q        string
	Sort     string
	Order    string
}

// indicatorSortCol maps the FE sort param to a whitelisted column.
func indicatorSortCol(sort string) string {
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

func listStatOrder(order string) string {
	if order == "desc" {
		return "DESC"
	}
	return "ASC"
}

// ListIndicators returns indicator catalog rows filtered by q, sorted by the
// whitelisted column.
func (r *StatisticalRepository) ListIndicators(ctx context.Context, params ListIndicatorsParams) ([]Indicator, error) {
	where := "tenant_id = $1 AND is_active"
	args := []any{params.TenantID}
	if params.Q != "" {
		args = append(args, "%"+params.Q+"%")
		where = fmt.Sprintf(
			"tenant_id = $1 AND is_active AND (code ILIKE $%d OR name ILIKE $%d)",
			len(args), len(args))
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, code, name, COALESCE(unit,''), COALESCE(group_code,''), is_active,
		       COALESCE(created_by,''), created_at, updated_at,
		       kpi_type, periodicity, is_ratio, root_code, root_name,
		       COALESCE(meaning,''), sources, formula, dimensions,
		       COALESCE(display_format,''), rounding_digits, COALESCE(score_group,''), weight
		FROM rpt_indicators WHERE %s ORDER BY %s %s`,
		where, indicatorSortCol(params.Sort), listStatOrder(params.Order)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Indicator{}
	for rows.Next() {
		var i Indicator
		if err := rows.Scan(&i.ID, &i.TenantID, &i.Code, &i.Name, &i.Unit, &i.GroupCode,
			&i.IsActive, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt,
			&i.KpiType, &i.Periodicity, &i.IsRatio, &i.RootCode, &i.RootName,
			&i.Meaning, &i.Sources, &i.Formula, &i.Dimensions,
			&i.DisplayFormat, &i.RoundingDigits, &i.ScoreGroup, &i.Weight); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// GetIndicatorByCode loads one active indicator by business code (compute
// engine entry point).
func (r *StatisticalRepository) GetIndicatorByCode(ctx context.Context, tenantID, code string) (*Indicator, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, COALESCE(unit,''), COALESCE(group_code,''), is_active,
		       COALESCE(created_by,''), created_at, updated_at,
		       kpi_type, periodicity, is_ratio, root_code, root_name,
		       COALESCE(meaning,''), sources, formula, dimensions,
		       COALESCE(display_format,''), rounding_digits, COALESCE(score_group,''), weight
		FROM rpt_indicators WHERE tenant_id = $1 AND code = $2 AND is_active`, tenantID, code)
	var i Indicator
	if err := row.Scan(&i.ID, &i.TenantID, &i.Code, &i.Name, &i.Unit, &i.GroupCode,
		&i.IsActive, &i.CreatedBy, &i.CreatedAt, &i.UpdatedAt,
		&i.KpiType, &i.Periodicity, &i.IsRatio, &i.RootCode, &i.RootName,
		&i.Meaning, &i.Sources, &i.Formula, &i.Dimensions,
		&i.DisplayFormat, &i.RoundingDigits, &i.ScoreGroup, &i.Weight); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

// ScalarQuery runs a builder-generated scalar aggregate (one row, one numeric
// column) and returns the value. The SQL comes from the formula engine's own
// builders — never from stored text or caller input.
func (r *StatisticalRepository) ScalarQuery(ctx context.Context, query string, args []any) (float64, error) {
	var value sql.NullFloat64
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	if !value.Valid {
		return 0, nil
	}
	return value.Float64, nil
}

// TrialBalanceTotals returns the end-of-day debit and credit totals for the
// latest trial-balance snapshot on or before the period end. They must be
// equal in a double-entry book; the reconciliation uses the pair as the
// baseline check on the fact and ETL.
func (r *StatisticalRepository) TrialBalanceTotals(ctx context.Context, tenantID, periodCode string) (int64, int64, error) {
	var debit, credit sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(close_debit_minor),0), COALESCE(SUM(close_credit_minor),0)
		FROM rpt_fact_trial_balance_daily
		WHERE tenant_id = $1
		  AND business_date = (
		    SELECT max(business_date) FROM rpt_fact_trial_balance_daily
		    WHERE tenant_id = $1
		      AND business_date <= ($2::date + INTERVAL '1 month' - INTERVAL '1 day')::date)`,
		tenantID, periodCode+"-01").Scan(&debit, &credit); err != nil {
		return 0, 0, err
	}
	return debit.Int64, credit.Int64, nil
}

// UpsertIndicator creates or updates one indicator.
func (r *StatisticalRepository) UpsertIndicator(ctx context.Context, i *Indicator) (*Indicator, error) {
	if i.ID == "" {
		i.ID = NewStatisticalID("ind")
	}
	if i.KpiType == "" {
		i.KpiType = "P"
	}
	if i.Periodicity == "" {
		i.Periodicity = "D"
	}
	if i.Sources == nil {
		i.Sources = []byte("[]")
	}
	if i.Formula == nil {
		i.Formula = []byte("{}")
	}
	if i.Dimensions == nil {
		i.Dimensions = []byte("[]")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_indicators (id, tenant_id, code, name, unit, group_code, created_by,
			kpi_type, periodicity, is_ratio, root_code, root_name, meaning,
			sources, formula, dimensions, display_format, rounding_digits, score_group, weight)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name, unit = EXCLUDED.unit,
			group_code = EXCLUDED.group_code, kpi_type = EXCLUDED.kpi_type,
			periodicity = EXCLUDED.periodicity, is_ratio = EXCLUDED.is_ratio,
			root_code = EXCLUDED.root_code, root_name = EXCLUDED.root_name,
			meaning = EXCLUDED.meaning, sources = EXCLUDED.sources, formula = EXCLUDED.formula,
			dimensions = EXCLUDED.dimensions, display_format = EXCLUDED.display_format,
			rounding_digits = EXCLUDED.rounding_digits, score_group = EXCLUDED.score_group,
			weight = EXCLUDED.weight,
			updated_at = now(), version = rpt_indicators.version + 1
		RETURNING created_at, updated_at`,
		i.ID, i.TenantID, i.Code, i.Name, nullStringStat(i.Unit), nullStringStat(i.GroupCode), i.CreatedBy,
		i.KpiType, i.Periodicity, i.IsRatio, i.RootCode, i.RootName, i.Meaning,
		i.Sources, i.Formula, i.Dimensions, i.DisplayFormat, i.RoundingDigits,
		nullStringStat(i.ScoreGroup), i.Weight)
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

// MarkSubmissionSubmitted stamps the workflow case and moves DRAFT→SUBMITTED.
func (r *StatisticalRepository) MarkSubmissionSubmitted(ctx context.Context, tenantID, id, caseID, actor string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE rpt_report_submissions
		SET status = 'SUBMITTED', workflow_case_id = NULLIF($3,'')::uuid, submitted_by = $4,
		    submitted_at = now(), updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid AND status = 'DRAFT'`,
		tenantID, id, caseID, actor)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ResolveSubmission moves SUBMITTED→APPROVED|REJECTED. Only the first decision
// wins, so a retried callback is a no-op rather than an overwrite.
// ErrStaleVersion marks a guarded decision whose row version no longer matches
// the one the checker saw (the submission changed while in review).
var ErrStaleVersion = errors.New("rpt: dossier changed while in review")

func (r *StatisticalRepository) ResolveSubmission(ctx context.Context, tenantID, id, status, actor string, dataVersion int64) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE rpt_report_submissions
		SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid AND status = 'SUBMITTED'
		  AND ($5 = 0 OR version = $5)`,
		tenantID, id, status, actor, dataVersion)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if dataVersion > 0 {
		var current int64
		if e := r.db.QueryRowContext(ctx, `SELECT version FROM rpt_report_submissions WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id).Scan(&current); e == nil && current != dataVersion {
			return false, ErrStaleVersion
		}
	}
	return false, nil
}

// GetReportDefinitionByCode loads one active definition by code.
func (r *StatisticalRepository) GetReportDefinitionByCode(ctx context.Context, tenantID, code string) (*ReportDefinition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, COALESCE(group_code,''), query_id, param_schema,
		       template_file_id::text, output_format, is_active, COALESCE(created_by,''),
		       COALESCE(updated_by,''), created_at, updated_at
		FROM rpt_report_definitions WHERE tenant_id = $1 AND code = $2 AND is_active`, tenantID, code)
	var d ReportDefinition
	var template, group sql.NullString
	err := row.Scan(&d.ID, &d.TenantID, &d.Code, &d.Name, &group, &d.QueryID, &d.ParamSchema,
		&template, &d.OutputFormat, &d.IsActive, &d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.GroupCode = group.String
	if template.Valid {
		d.TemplateFileID = &template.String
	}
	return &d, nil
}

// RunQuery executes one builder-provided parameterised query and returns the
// rows as aligned values (the SQL text never contains caller input).
func (r *StatisticalRepository) RunQuery(ctx context.Context, query string, args []any, columns []string) ([][]any, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, values)
	}
	return out, rows.Err()
}

// ListSubmissionsParams carries the parsed list query for submissions.
type ListSubmissionsParams struct {
	TenantID   string
	ReportCode string
	PeriodCode string
	Status     string // comma list of DRAFT|SUBMITTED|APPROVED|REJECTED
	Sort       string
	Order      string
	Page       int
	Size       int
}

// submissionSortCol maps the FE sort param to a whitelisted column.
func submissionSortCol(sort string) string {
	switch sort {
	case "report_code":
		return "report_code"
	case "period_code":
		return "period_code"
	case "status":
		return "status"
	case "created_at":
		return "created_at"
	default:
		return "created_at"
	}
}

// ListSubmissions returns a paged slice of submissions filtered by report,
// period and status (comma list).
func (r *StatisticalRepository) ListSubmissions(ctx context.Context, params ListSubmissionsParams) ([]ReportSubmission, int, error) {
	where := []string{"tenant_id = $1"}
	args := []any{params.TenantID}
	idx := 2
	if params.ReportCode != "" {
		where = append(where, fmt.Sprintf("report_code ILIKE '%%' || $%d::text || '%%'", idx))
		args = append(args, params.ReportCode)
		idx++
	}
	if params.PeriodCode != "" {
		where = append(where, fmt.Sprintf("period_code ILIKE '%%' || $%d::text || '%%'", idx))
		args = append(args, params.PeriodCode)
		idx++
	}
	if params.Status != "" {
		where = append(where, fmt.Sprintf("status = ANY(string_to_array($%d, ','))", idx))
		args = append(args, params.Status)
		idx++
	}
	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM rpt_report_submissions WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	size := params.Size
	if size < 1 || size > 100 {
		size = 100
	}
	offset := params.Page
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, report_code, period_code, status, payload,
		       workflow_case_id::text, COALESCE(submitted_by,''), submitted_at, COALESCE(created_by,''), created_at, updated_at
		FROM rpt_report_submissions
		WHERE %s
		ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		wc, submissionSortCol(params.Sort), listStatOrder(params.Order), idx, idx+1),
		append(args, size, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ReportSubmission{}
	for rows.Next() {
		var sub ReportSubmission
		var caseID sql.NullString
		var submittedAt sql.NullTime
		if err := rows.Scan(&sub.ID, &sub.TenantID, &sub.ReportCode, &sub.PeriodCode, &sub.Status,
			&sub.Payload, &caseID, &sub.SubmittedBy, &submittedAt, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if caseID.Valid {
			sub.WorkflowCaseID = &caseID.String
		}
		if submittedAt.Valid {
			sub.SubmittedAt = &submittedAt.Time
		}
		out = append(out, sub)
	}
	return out, total, rows.Err()
}

// GetSubmissionByID loads one submission by id.
func (r *StatisticalRepository) GetSubmissionByID(ctx context.Context, tenantID, id string) (*ReportSubmission, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, report_code, period_code, status, payload,
		       workflow_case_id::text, COALESCE(submitted_by,''), submitted_at, COALESCE(created_by,''), created_at, updated_at,
		       version
		FROM rpt_report_submissions WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	var sub ReportSubmission
	var caseID sql.NullString
	var submittedAt sql.NullTime
	err := row.Scan(&sub.ID, &sub.TenantID, &sub.ReportCode, &sub.PeriodCode, &sub.Status,
		&sub.Payload, &caseID, &sub.SubmittedBy, &submittedAt, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt,
		&sub.DataVersion)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if caseID.Valid {
		sub.WorkflowCaseID = &caseID.String
	}
	if submittedAt.Valid {
		sub.SubmittedAt = &submittedAt.Time
	}
	return &sub, nil
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
