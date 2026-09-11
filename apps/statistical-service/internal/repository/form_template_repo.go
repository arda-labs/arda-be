package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// FormTemplate is one QCMS form/template row (W5b).
type FormTemplate struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	MediaFileID      *string         `json:"media_file_id,omitempty"`
	Schema           json.RawMessage `json:"schema"`
	WorkflowCaseType string          `json:"workflow_case_type,omitempty"`
	IsActive         bool            `json:"is_active"`
	CreatedBy        string          `json:"created_by"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// ListFormTemplates returns the template catalog.
func (r *StatisticalRepository) ListFormTemplates(ctx context.Context, tenantID string, includeInactive bool) ([]FormTemplate, error) {
	active := ""
	if !includeInactive {
		active = " AND is_active"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, code, name, media_file_id::text, schema, workflow_case_type,
		       is_active, COALESCE(created_by,''), created_at, updated_at
		FROM rpt_form_templates WHERE tenant_id = $1`+active+` ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FormTemplate{}
	for rows.Next() {
		var x FormTemplate
		var media sql.NullString
		if err := rows.Scan(&x.ID, &x.TenantID, &x.Code, &x.Name, &media, &x.Schema,
			&x.WorkflowCaseType, &x.IsActive, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		if media.Valid {
			x.MediaFileID = &media.String
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetFormTemplateByCode loads one template for export.
func (r *StatisticalRepository) GetFormTemplateByCode(ctx context.Context, tenantID, code string) (*FormTemplate, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, tenant_id, code, name, media_file_id::text, schema, workflow_case_type,
		       is_active, COALESCE(created_by,''), created_at, updated_at
		FROM rpt_form_templates WHERE tenant_id = $1 AND code = $2`, tenantID, code)
	var x FormTemplate
	var media sql.NullString
	err := row.Scan(&x.ID, &x.TenantID, &x.Code, &x.Name, &media, &x.Schema,
		&x.WorkflowCaseType, &x.IsActive, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if media.Valid {
		x.MediaFileID = &media.String
	}
	return &x, nil
}

// UpsertFormTemplate creates or updates one template by (tenant, code).
func (r *StatisticalRepository) UpsertFormTemplate(ctx context.Context, in *FormTemplate) (*FormTemplate, error) {
	if len(in.Schema) == 0 {
		in.Schema = json.RawMessage(`{}`)
	}
	var media any
	if in.MediaFileID != nil && *in.MediaFileID != "" {
		media = *in.MediaFileID
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_form_templates (tenant_id, code, name, media_file_id, schema,
			workflow_case_type, is_active, created_by)
		VALUES ($1,$2,$3,$4::uuid,$5,$6,COALESCE($7,true),$8)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name,
			media_file_id = EXCLUDED.media_file_id, schema = EXCLUDED.schema,
			workflow_case_type = EXCLUDED.workflow_case_type, is_active = EXCLUDED.is_active,
			updated_at = now(), version = rpt_form_templates.version + 1
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.Code, in.Name, media, in.Schema, in.WorkflowCaseType, in.IsActive, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}
