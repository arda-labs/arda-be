package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GeneralProvisionRow is one LNM.307 per-org provision period (the approved
// row is the cumulative provision source for the next period).
type GeneralProvisionRow struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	OrgCode          string  `json:"org_code"`
	ProvisionDate    string  `json:"provision_date"`
	RatePercent      float64 `json:"rate_percent"`
	TotalOutstanding int64   `json:"total_outstanding_minor"`
	Accum            int64   `json:"accum_provision_minor"`
	Required         int64   `json:"required_provision_minor"`
	Alloc            int64   `json:"alloc_minor"`
	Reverse          int64   `json:"reverse_minor"`
	Status           string  `json:"status"`
	WorkflowCaseID   string  `json:"workflow_case_id,omitempty"`
	WorkflowCaseCode string  `json:"workflow_case_code,omitempty"`
	JournalEntryID   string  `json:"journal_entry_id,omitempty"`
	CreatedBy        string  `json:"created_by"`
	CreatedAt        string  `json:"created_at"`
}

const generalProvisionColumns = `
	id, tenant_id, org_code, provision_date::text, rate_percent::float8,
	total_outstanding_minor, accum_provision_minor, required_provision_minor,
	alloc_minor, reverse_minor, status,
	COALESCE(workflow_case_id,''), COALESCE(workflow_case_code,''),
	COALESCE(journal_entry_id::text,''), created_by, created_at::text`

func scanGeneralProvision(scan func(...any) error) (*GeneralProvisionRow, error) {
	var row GeneralProvisionRow
	if err := scan(&row.ID, &row.TenantID, &row.OrgCode, &row.ProvisionDate, &row.RatePercent,
		&row.TotalOutstanding, &row.Accum, &row.Required, &row.Alloc, &row.Reverse,
		&row.Status, &row.WorkflowCaseID, &row.WorkflowCaseCode, &row.JournalEntryID,
		&row.CreatedBy, &row.CreatedAt); err != nil {
		return nil, err
	}
	return &row, nil
}

// InsertGeneralProvision stores a DRAFT period row.
func (r *LoanRepository) InsertGeneralProvision(ctx context.Context, row GeneralProvisionRow) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO lnm_general_provisions
			(id, tenant_id, org_code, provision_date, rate_percent,
			 total_outstanding_minor, accum_provision_minor, required_provision_minor,
			 alloc_minor, reverse_minor, status, created_by)
		VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,$11,$12)`,
		row.ID, row.TenantID, row.OrgCode, row.ProvisionDate, row.RatePercent,
		row.TotalOutstanding, row.Accum, row.Required, row.Alloc, row.Reverse,
		row.Status, row.CreatedBy)
	return err
}

// GetGeneralProvision returns one period row scoped by tenant.
func (r *LoanRepository) GetGeneralProvision(ctx context.Context, tenantID, id string) (*GeneralProvisionRow, error) {
	row, err := scanGeneralProvision(r.db.QueryRowContext(ctx, `
		SELECT `+generalProvisionColumns+`
		FROM lnm_general_provisions WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("lnm: record not found")
	}
	return row, err
}

// ListGeneralProvisions lists period rows (optional org filter).
func (r *LoanRepository) ListGeneralProvisions(ctx context.Context, tenantID, orgCode string) ([]GeneralProvisionRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+generalProvisionColumns+`
		FROM lnm_general_provisions
		WHERE tenant_id = $1 AND ($2 = '' OR org_code = $2)
		ORDER BY provision_date DESC, org_code`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GeneralProvisionRow{}
	for rows.Next() {
		row, err := scanGeneralProvision(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// SetGeneralProvisionCase links the period to its workflow case (SUBMITTED).
func (r *LoanRepository) SetGeneralProvisionCase(ctx context.Context, tenantID, id, caseID, caseCode string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_general_provisions
		SET workflow_case_id = $3, workflow_case_code = $4, status = 'SUBMITTED',
		    updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, caseID, caseCode)
	if err != nil {
		return err
	}
	return expectOneRow(res, "general provision")
}

// SettleGeneralProvision marks the period POSTED with its recomputed figures.
func (r *LoanRepository) SettleGeneralProvision(ctx context.Context, tenantID, id string, total, accum, required, alloc, reverse int64, journalEntryID, actor string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_general_provisions
		SET total_outstanding_minor = $3, accum_provision_minor = $4,
		    required_provision_minor = $5, alloc_minor = $6, reverse_minor = $7,
		    status = 'POSTED', journal_entry_id = NULLIF($8,'')::uuid,
		    updated_by = $9, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'SUBMITTED'`,
		tenantID, id, total, accum, required, alloc, reverse, journalEntryID, actor)
	if err != nil {
		return err
	}
	return expectOneRow(res, "general provision")
}

// ResolveGeneralProvision closes the period without posting (REJECT/CANCEL).
func (r *LoanRepository) ResolveGeneralProvision(ctx context.Context, tenantID, id, status, actor string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_general_provisions
		SET status = $3, updated_by = $4, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'SUBMITTED'`,
		tenantID, id, status, actor)
	if err != nil {
		return err
	}
	return expectOneRow(res, "general provision")
}

// GeneralProvisionRate returns the effective rate for an org ('%' fallback).
func (r *LoanRepository) GeneralProvisionRate(ctx context.Context, orgCode string) (float64, error) {
	var rate float64
	err := r.db.QueryRowContext(ctx, `
		SELECT rate_percent::float8 FROM lnm_general_provision_rates
		WHERE org_code = $1`, orgCode).Scan(&rate)
	if err == nil {
		return rate, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT rate_percent::float8 FROM lnm_general_provision_rates
		WHERE org_code = '%'`).Scan(&rate); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return rate, nil
}

// LatestPostedRequired is the cumulative provision for the org as of a date.
func (r *LoanRepository) LatestPostedRequired(ctx context.Context, tenantID, orgCode, asOf string) (int64, error) {
	var value sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT required_provision_minor FROM lnm_general_provisions
		WHERE tenant_id = $1 AND org_code = $2 AND status = 'POSTED'
		  AND provision_date <= $3::date
		ORDER BY provision_date DESC LIMIT 1`, tenantID, orgCode, asOf).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return value.Int64, nil
}

// SumGeneralProvisionOutstanding sums active agreement outstanding for the
// org ('' = all orgs) as of the provision date.
func (r *LoanRepository) SumGeneralProvisionOutstanding(ctx context.Context, tenantID, orgCode, asOf string) (int64, error) {
	var total sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(a.outstanding_amt_minor), 0)
		FROM lnm_agreements a
		JOIN lnm_contracts c
		  ON c.tenant_id = a.tenant_id AND c.contract_code = a.contract_code
		WHERE a.tenant_id = $1 AND a.status = 'ACTIVE' AND a.outstanding_amt_minor > 0
		  AND a.disburse_date <= $3::date
		  AND c.status <> 'CLOSED'
		  AND ($2 = '' OR c.employee_code = $2)`, tenantID, orgCode, asOf).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

func expectOneRow(res sql.Result, entity string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("lnm: %s not found or not actionable", entity)
	}
	return nil
}
