package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LoanPlan is one loan plan catalog row (W7).
type LoanPlan struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	Code              string    `json:"code"`
	Name              string    `json:"name"`
	FromDate          string    `json:"from_date,omitempty"`
	ToDate            string    `json:"to_date,omitempty"`
	TargetAmountMinor int64     `json:"target_amount_minor"`
	Note              string    `json:"note,omitempty"`
	Status            string    `json:"status"`
	OrgCode           string    `json:"org_code,omitempty"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ListPlans returns the loan plan catalog.
func (r *LoanRepository) ListPlans(ctx context.Context, tenantID, status string) ([]LoanPlan, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, COALESCE(from_date::text,''), COALESCE(to_date::text,''),
		       target_amount_minor, COALESCE(note,''), status, COALESCE(org_code,''),
		       COALESCE(created_by,''), created_at, updated_at
		FROM lnm_plans WHERE `+strings.Join(where, " AND ")+`
		ORDER BY code LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoanPlan{}
	for rows.Next() {
		var p LoanPlan
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.FromDate, &p.ToDate,
			&p.TargetAmountMinor, &p.Note, &p.Status, &p.OrgCode, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPlan creates or updates one plan by (tenant, code).
func (r *LoanRepository) UpsertPlan(ctx context.Context, p *LoanPlan) (*LoanPlan, error) {
	if p.ID == "" {
		p.ID = NewID("lnmplan")
	}
	if p.Status == "" {
		p.Status = "ACTIVE"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_plans (id, tenant_id, code, name, from_date, to_date,
			target_amount_minor, note, status, org_code, created_by)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::date,NULLIF($6,'')::date,$7,$8,$9,NULLIF($10,''),$11)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name,
			from_date = EXCLUDED.from_date, to_date = EXCLUDED.to_date,
			target_amount_minor = EXCLUDED.target_amount_minor, note = EXCLUDED.note,
			status = EXCLUDED.status, org_code = EXCLUDED.org_code,
			updated_at = now(), version = lnm_plans.version + 1
		RETURNING created_at, updated_at`,
		p.ID, p.TenantID, p.Code, p.Name, p.FromDate, p.ToDate, p.TargetAmountMinor,
		p.Note, p.Status, p.OrgCode, p.CreatedBy)
	if err := row.Scan(&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// SetPlanStatus toggles one plan by id.
func (r *LoanRepository) SetPlanStatus(ctx context.Context, tenantID, id, status string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_plans SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
