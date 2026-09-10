package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ProductRequest is one staged DPM product register/edit payload awaiting the
// maker/checker decision (DPM.102/103).
type ProductRequest struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	RequestType      string  `json:"request_type"`
	ProductCode      string  `json:"product_code"`
	Name             string  `json:"name"`
	TermMonths       int     `json:"term_months"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	Status           string  `json:"status"`
	WorkflowCaseID   string  `json:"workflow_case_id,omitempty"`
	WorkflowCaseCode string  `json:"workflow_case_code,omitempty"`
	CreatedBy        string  `json:"created_by"`
	CreatedAt        string  `json:"created_at"`
}

const productRequestColumns = `id, tenant_id, request_type, product_code, name, term_months,
	interest_rate::float8, currency_code, status,
	COALESCE(workflow_case_id,''), COALESCE(workflow_case_code,''), created_by, created_at::text`

func scanProductRequest(scan func(...any) error) (*ProductRequest, error) {
	var p ProductRequest
	if err := scan(&p.ID, &p.TenantID, &p.RequestType, &p.ProductCode, &p.Name, &p.TermMonths,
		&p.InterestRate, &p.CurrencyCode, &p.Status, &p.WorkflowCaseID, &p.WorkflowCaseCode,
		&p.CreatedBy, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// InsertProductRequest stores a SUBMITTED request row.
func (r *DepositRepository) InsertProductRequest(ctx context.Context, p *ProductRequest) (*ProductRequest, error) {
	if p.ID == "" {
		p.ID = NewDepositID("dpmprq")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_product_requests
			(id, tenant_id, request_type, product_code, name, term_months, interest_rate, currency_code, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'SUBMITTED',$9)
		RETURNING created_at::text`,
		p.ID, p.TenantID, p.RequestType, p.ProductCode, p.Name, p.TermMonths,
		p.InterestRate, p.CurrencyCode, p.CreatedBy)
	if err := row.Scan(&p.CreatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// GetProductRequest returns one request row scoped by tenant.
func (r *DepositRepository) GetProductRequest(ctx context.Context, tenantID, id string) (*ProductRequest, error) {
	row, err := scanProductRequest(r.db.QueryRowContext(ctx, `
		SELECT `+productRequestColumns+`
		FROM dpm_product_requests WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("dpm: record not found")
	}
	return row, err
}

// ListProductRequests lists requests (optional status filter).
func (r *DepositRepository) ListProductRequests(ctx context.Context, tenantID, status string) ([]ProductRequest, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+productRequestColumns+`
		FROM dpm_product_requests
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC`, tenantID, strings.TrimSpace(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProductRequest{}
	for rows.Next() {
		item, err := scanProductRequest(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// SetProductRequestCase links the request to its workflow case.
func (r *DepositRepository) SetProductRequestCase(ctx context.Context, tenantID, id, caseID, caseCode string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE dpm_product_requests
		SET workflow_case_id = $3, workflow_case_code = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, caseID, caseCode)
	if err != nil {
		return err
	}
	return productRequestAffected(res)
}

// MarkProductRequestResolved closes a SUBMITTED request (APPLIED | REJECTED).
func (r *DepositRepository) MarkProductRequestResolved(ctx context.Context, tenantID, id, status string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE dpm_product_requests
		SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'SUBMITTED'`, tenantID, id, status)
	if err != nil {
		return err
	}
	return productRequestAffected(res)
}

func productRequestAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("dpm: product request not found or not actionable")
	}
	return nil
}
