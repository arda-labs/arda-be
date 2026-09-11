package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SpecificProvisionRow is one LNM.306 per-agreement provision request (W7).
type SpecificProvisionRow struct {
	ID               string
	TenantID         string
	ContractCode     string
	AgreementCode    string
	ProvisionDate    string
	OutstandingMinor int64
	DebtGroupCode    string
	RatePercent      float64
	DeductionMinor   int64
	BaseMinor        int64
	AmountMinor      int64
	Status           string
	WorkflowCaseID   string
	WorkflowCaseCode string
	JournalEntryID   string
	CreatedBy        string
	CreatedAt        string
}

const specificProvisionColumns = `
	id, tenant_id, contract_code, agreement_code, provision_date::text,
	outstanding_minor, debt_group_code, rate_percent::float8, deduction_minor,
	base_minor, amount_minor, status, COALESCE(workflow_case_id::text,''),
	COALESCE(workflow_case_code,''), COALESCE(journal_entry_id::text,''),
	created_by, created_at::text`

func scanSpecificProvision(scan func(...any) error) (*SpecificProvisionRow, error) {
	var row SpecificProvisionRow
	if err := scan(&row.ID, &row.TenantID, &row.ContractCode, &row.AgreementCode, &row.ProvisionDate,
		&row.OutstandingMinor, &row.DebtGroupCode, &row.RatePercent, &row.DeductionMinor,
		&row.BaseMinor, &row.AmountMinor, &row.Status, &row.WorkflowCaseID,
		&row.WorkflowCaseCode, &row.JournalEntryID, &row.CreatedBy, &row.CreatedAt); err != nil {
		return nil, err
	}
	return &row, nil
}

// InsertSpecificProvision stores one SUBMITTED request.
func (r *LoanRepository) InsertSpecificProvision(ctx context.Context, row SpecificProvisionRow) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO lnm_specific_provisions
			(id, tenant_id, contract_code, agreement_code, provision_date, outstanding_minor,
			 debt_group_code, rate_percent, deduction_minor, base_minor, amount_minor, status, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$13)`,
		row.ID, row.TenantID, row.ContractCode, row.AgreementCode, row.ProvisionDate,
		row.OutstandingMinor, row.DebtGroupCode, row.RatePercent, row.DeductionMinor,
		row.BaseMinor, row.AmountMinor, row.Status, row.CreatedBy)
	return err
}

// GetSpecificProvision returns one request.
func (r *LoanRepository) GetSpecificProvision(ctx context.Context, tenantID, id string) (*SpecificProvisionRow, error) {
	row, err := scanSpecificProvision(r.db.QueryRowContext(ctx, `
		SELECT `+specificProvisionColumns+`
		FROM lnm_specific_provisions WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("lnm: record not found")
	}
	return row, err
}

// ListSpecificProvisions lists requests (optional status filter).
func (r *LoanRepository) ListSpecificProvisions(ctx context.Context, tenantID, status string) ([]SpecificProvisionRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+specificProvisionColumns+`
		FROM lnm_specific_provisions
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY provision_date DESC, agreement_code LIMIT 500`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SpecificProvisionRow{}
	for rows.Next() {
		row, err := scanSpecificProvision(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// SetSpecificProvisionCase links the request to its workflow case.
func (r *LoanRepository) SetSpecificProvisionCase(ctx context.Context, tenantID, id, caseID, caseCode string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_specific_provisions
		SET workflow_case_id = NULLIF($3,'')::uuid, workflow_case_code = $4, updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, caseID, caseCode)
	if err != nil {
		return err
	}
	return expectOneRow(res, "specific provision")
}

// SettleSpecificProvision marks the request POSTED.
func (r *LoanRepository) SettleSpecificProvision(ctx context.Context, tenantID, id string, amount int64, journalEntryID, actor string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_specific_provisions
		SET amount_minor = $3, status = 'POSTED', journal_entry_id = NULLIF($4,'')::uuid,
		    updated_by = $5, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'SUBMITTED'`,
		tenantID, id, amount, journalEntryID, actor)
	if err != nil {
		return err
	}
	return expectOneRow(res, "specific provision")
}

// ResolveSpecificProvision closes the request without posting.
func (r *LoanRepository) ResolveSpecificProvision(ctx context.Context, tenantID, id, status, actor string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_specific_provisions
		SET status = $3, updated_by = $4, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'SUBMITTED'`, tenantID, id, status, actor)
	if err != nil {
		return err
	}
	return expectOneRow(res, "specific provision")
}

// AgreementProvisionBase returns (outstanding, debt_group, contract_code).
func (r *LoanRepository) AgreementProvisionBase(ctx context.Context, tenantID, agreementCode string) (int64, string, string, error) {
	var outstanding int64
	var debtGroup, contractCode string
	err := r.db.QueryRowContext(ctx, `
		SELECT outstanding_amt_minor, debt_group_code, contract_code
		FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`,
		tenantID, agreementCode).Scan(&outstanding, &debtGroup, &contractCode)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", fmt.Errorf("lnm: agreement not found")
	}
	if err != nil {
		return 0, "", "", err
	}
	return outstanding, debtGroup, contractCode, nil
}

// DebtGroupProvisionRate reads the seeded TT 02/2023 rate (lnm_provision_rates).
func (r *LoanRepository) DebtGroupProvisionRate(ctx context.Context, debtGroupCode string) (float64, error) {
	var rate float64
	err := r.db.QueryRowContext(ctx, `
		SELECT rate_percent::float8 FROM lnm_provision_rates WHERE debt_group_code = $1`,
		debtGroupCode).Scan(&rate)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return rate, nil
}

// CollateralDeduction sums coll_value × deduction_ratio for one contract.
func (r *LoanRepository) CollateralDeduction(ctx context.Context, tenantID, contractCode string) (int64, error) {
	var total sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(cc.coll_value_minor * COALESCE(c.deduction_ratio, 0)), 0)
		FROM lnm_contract_collaterals cc
		LEFT JOIN lnm_collaterals c
		  ON c.tenant_id = cc.tenant_id AND c.coll_code = cc.coll_code
		WHERE cc.tenant_id = $1 AND cc.contract_code = $2`, tenantID, contractCode).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}
