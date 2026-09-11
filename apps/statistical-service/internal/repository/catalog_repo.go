package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CatalogItem is one QCMS catalog row (W5) — kind selects the EPAS screen.
type CatalogItem struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenant_id"`
	Kind       string          `json:"kind"`
	Code       string          `json:"code"`
	Name       string          `json:"name"`
	ParentCode string          `json:"parent_code,omitempty"`
	Attributes json.RawMessage `json:"attributes"`
	IsActive   bool            `json:"is_active"`
	CreatedBy  string          `json:"created_by"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// ListCatalogItems returns rows of one kind (optionally filtered by q).
func (r *StatisticalRepository) ListCatalogItems(ctx context.Context, tenantID, kind, q string, includeInactive bool) ([]CatalogItem, error) {
	where := []string{"tenant_id = $1", "kind = $2"}
	args := []any{tenantID, kind}
	if !includeInactive {
		where = append(where, "is_active")
	}
	if q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", len(args), len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, kind, code, name, parent_code, attributes, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM rpt_catalog_items WHERE `+strings.Join(where, " AND ")+`
		ORDER BY code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CatalogItem{}
	for rows.Next() {
		var x CatalogItem
		if err := rows.Scan(&x.ID, &x.TenantID, &x.Kind, &x.Code, &x.Name, &x.ParentCode,
			&x.Attributes, &x.IsActive, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// UpsertCatalogItem creates or updates one row by (tenant, kind, code).
func (r *StatisticalRepository) UpsertCatalogItem(ctx context.Context, in *CatalogItem) (*CatalogItem, error) {
	if len(in.Attributes) == 0 {
		in.Attributes = json.RawMessage(`{}`)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_catalog_items (tenant_id, kind, code, name, parent_code, attributes, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,COALESCE($7,true),$8)
		ON CONFLICT (tenant_id, kind, code) DO UPDATE SET
			name = EXCLUDED.name, parent_code = EXCLUDED.parent_code, attributes = EXCLUDED.attributes,
			is_active = EXCLUDED.is_active, updated_at = now(), version = rpt_catalog_items.version + 1
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.Kind, in.Code, in.Name, in.ParentCode, in.Attributes, in.IsActive, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// SetCatalogItemActive toggles the soft-delete flag by id+kind.
func (r *StatisticalRepository) SetCatalogItemActive(ctx context.Context, tenantID, kind, id string, active bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE rpt_catalog_items SET is_active = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND kind = $2 AND id = $3::uuid`, tenantID, kind, id, active)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("catalog item not found")
	}
	return nil
}

// SubmissionStatusCounts returns submission counts grouped by status.
func (r *StatisticalRepository) SubmissionStatusCounts(ctx context.Context, tenantID string) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM rpt_report_submissions
		WHERE tenant_id = $1 GROUP BY status`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}

// CatalogKindCounts returns catalog item counts grouped by kind.
func (r *StatisticalRepository) CatalogKindCounts(ctx context.Context, tenantID string) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT kind, COUNT(*) FROM rpt_catalog_items
		WHERE tenant_id = $1 AND is_active GROUP BY kind ORDER BY kind`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		out[kind] = count
	}
	return out, rows.Err()
}

// ActiveCounts returns (definitions, indicators, forms) active row counts.
func (r *StatisticalRepository) ActiveCounts(ctx context.Context, tenantID string) (int, int, int, error) {
	var definitions, indicators, forms int
	if err := r.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM rpt_report_definitions WHERE tenant_id = $1 AND is_active),
			(SELECT COUNT(*) FROM rpt_indicators WHERE tenant_id = $1 AND is_active),
			(SELECT COUNT(*) FROM rpt_form_templates WHERE tenant_id = $1 AND is_active)`,
		tenantID).Scan(&definitions, &indicators, &forms); err != nil {
		return 0, 0, 0, err
	}
	return definitions, indicators, forms, nil
}

var _ = sql.ErrNoRows
