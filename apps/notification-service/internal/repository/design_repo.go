package repository

import (
	"context"
	"database/sql"
	"time"
)

// EmailDesign is one reusable HTML email layout. Event templates reference it
// through noti_templates.design_code so one layout serves many events.
type EmailDesign struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	BodyHTML  string    `json:"body_html"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *NotificationRepository) ListEmailDesigns(ctx context.Context, tenantID string) ([]EmailDesign, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, code, name, subject, body_html, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM noti_email_designs WHERE tenant_id = $1 ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmailDesign{}
	for rows.Next() {
		var d EmailDesign
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Code, &d.Name, &d.Subject, &d.BodyHTML,
			&d.IsActive, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *NotificationRepository) UpsertEmailDesign(ctx context.Context, in *EmailDesign) (*EmailDesign, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO noti_email_designs (tenant_id, code, name, subject, body_html, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,COALESCE($6,true),$7)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
			name = EXCLUDED.name, subject = EXCLUDED.subject, body_html = EXCLUDED.body_html,
			is_active = EXCLUDED.is_active, updated_at = now(), version = noti_email_designs.version + 1
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.Code, in.Name, in.Subject, in.BodyHTML, in.IsActive, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

func (r *NotificationRepository) DeleteEmailDesign(ctx context.Context, tenantID, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM noti_email_designs WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// FindEmailDesign returns the active design for a code.
func (r *NotificationRepository) FindEmailDesign(ctx context.Context, tenantID, code string) (*EmailDesign, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, tenant_id, code, name, subject, body_html, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM noti_email_designs WHERE tenant_id = $1 AND code = $2 AND is_active LIMIT 1`,
		tenantID, code)
	var d EmailDesign
	err := row.Scan(&d.ID, &d.TenantID, &d.Code, &d.Name, &d.Subject, &d.BodyHTML,
		&d.IsActive, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
