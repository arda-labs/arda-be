package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

var (
	ErrProvisionRateMissing        = errors.New("lnm: provision rate missing")
	ErrSpecificProvisionNotPending = errors.New("lnm: specific provision is not pending")
	ErrAgreementClosedForProvision = errors.New("lnm: closed agreement cannot be provisioned")
)

// SpecificProvisionDelta returns the positive ADD and REVERSAL journal values.
func SpecificProvisionDelta(required, current int64) (add, reversal int64) {
	switch {
	case required > current:
		return required - current, 0
	case current > required:
		return 0, current - required
	default:
		return 0, 0
	}
}

// SpecificProvisionRow is one LNM.306 per-agreement provision request (W7).
type SpecificProvisionRow struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	ContractCode     string  `json:"contract_code"`
	AgreementCode    string  `json:"agreement_code"`
	ProvisionDate    string  `json:"provision_date"`
	OutstandingMinor int64   `json:"outstanding_minor"`
	DebtGroupCode    string  `json:"debt_group_code"`
	RatePercent      float64 `json:"rate_percent"`
	DeductionMinor   int64   `json:"deduction_minor"`
	BaseMinor        int64   `json:"base_minor"`
	AmountMinor      int64   `json:"amount_minor"`
	Status           string  `json:"status"`
	WorkflowCaseID   string  `json:"workflow_case_id,omitempty"`
	WorkflowCaseCode string  `json:"workflow_case_code,omitempty"`
	JournalEntryID   string  `json:"journal_entry_id,omitempty"`
	CreatedBy        string  `json:"created_by"`
	CreatedAt        string  `json:"created_at"`
	// DataVersion is the row version the checker saw (lnm_specific_provisions.version).
	DataVersion int64 `json:"data_version"`
}

const specificProvisionColumns = `
	id, tenant_id, contract_code, agreement_code, provision_date::text,
	outstanding_minor, debt_group_code, rate_percent::float8, deduction_minor,
	base_minor, amount_minor, status, COALESCE(workflow_case_id::text,''),
	COALESCE(workflow_case_code,''), COALESCE(journal_entry_id::text,''),
	created_by, created_at::text, version`

func scanSpecificProvision(scan func(...any) error) (*SpecificProvisionRow, error) {
	var row SpecificProvisionRow
	if err := scan(&row.ID, &row.TenantID, &row.ContractCode, &row.AgreementCode, &row.ProvisionDate,
		&row.OutstandingMinor, &row.DebtGroupCode, &row.RatePercent, &row.DeductionMinor,
		&row.BaseMinor, &row.AmountMinor, &row.Status, &row.WorkflowCaseID,
		&row.WorkflowCaseCode, &row.JournalEntryID, &row.CreatedBy, &row.CreatedAt, &row.DataVersion); err != nil {
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

// SettleSpecificProvision serializes the provision balance change, ledger post,
// and dossier transition. The callback receives the signed delta while the
// agreement row is locked; its stable posting idempotency key makes retry safe
// if the database commit fails after Finance accepts the posting.
func (r *LoanRepository) SettleSpecificProvision(ctx context.Context, tenantID, id string, required int64, actor string, post func(delta int64) (string, error)) error {
	expected := domain.DataVersionFromContext(ctx)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var agreementCode, agreementStatus, contractStatus, provisionStatus string
	var current, version int64
	if err := tx.QueryRowContext(ctx, `
		SELECT p.agreement_code, p.status, a.status, c.status, a.provision_amt_minor, p.version
		FROM lnm_specific_provisions p
		JOIN lnm_agreements a ON a.tenant_id = p.tenant_id AND a.agreement_code = p.agreement_code
		JOIN lnm_contracts c ON c.tenant_id = p.tenant_id AND c.contract_code = p.contract_code
		WHERE p.tenant_id = $1 AND p.id = $2
		FOR UPDATE OF p, a, c`, tenantID, id).Scan(&agreementCode, &provisionStatus, &agreementStatus, &contractStatus, &current, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: specific provision or agreement", ErrNotFound)
		}
		return err
	}
	if provisionStatus != "SUBMITTED" {
		return ErrSpecificProvisionNotPending
	}
	if agreementStatus == "CLOSED" || contractStatus == "CLOSED" {
		return ErrAgreementClosedForProvision
	}
	if expected != 0 && version != expected {
		return ErrStaleVersion
	}
	add, reversal := SpecificProvisionDelta(required, current)
	delta := add - reversal
	journalEntryID := ""
	if delta != 0 && post == nil {
		return fmt.Errorf("lnm: specific provision posting is required for nonzero delta")
	}
	if delta != 0 {
		journalEntryID, err = post(delta)
		if err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE lnm_specific_provisions
		SET amount_minor = $3, status = 'POSTED', journal_entry_id = NULLIF($4,'')::uuid,
		    updated_by = $5, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'SUBMITTED' AND version = $6`,
		tenantID, id, required, journalEntryID, actor, version)
	if err != nil {
		return err
	}
	if err := expectOneRow(res, "specific provision"); err != nil {
		return err
	}
	res, err = tx.ExecContext(ctx, `
		UPDATE lnm_agreements
		SET provision_amt_minor = $3, updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, agreementCode, required)
	if err != nil {
		return err
	}
	if err := expectOneRow(res, "agreement"); err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveSpecificProvision closes the request without posting.
func (r *LoanRepository) ResolveSpecificProvision(ctx context.Context, tenantID, id, status, actor string) error {
	expected := domain.DataVersionFromContext(ctx)
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_specific_provisions
		SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'SUBMITTED'
		  AND ($5 = 0 OR version = $5)`, tenantID, id, status, actor, expected)
	if err != nil {
		return err
	}
	if err := expectOneRow(res, "specific provision"); err != nil {
		if staleVersion(ctx, r.db, "lnm_specific_provisions", tenantID, id, expected) {
			return ErrStaleVersion
		}
		return err
	}
	return nil
}

// AgreementProvisionBase returns (outstanding, debt_group, contract_code).
func (r *LoanRepository) AgreementProvisionBase(ctx context.Context, tenantID, agreementCode string) (int64, string, string, error) {
	var outstanding int64
	var debtGroup, contractCode, agreementStatus, contractStatus string
	err := r.db.QueryRowContext(ctx, `
		SELECT a.outstanding_amt_minor, a.debt_group_code, a.contract_code, a.status, c.status
		FROM lnm_agreements a
		JOIN lnm_contracts c ON c.tenant_id = a.tenant_id AND c.contract_code = a.contract_code
		WHERE a.tenant_id = $1 AND a.agreement_code = $2`,
		tenantID, agreementCode).Scan(&outstanding, &debtGroup, &contractCode, &agreementStatus, &contractStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", fmt.Errorf("lnm: agreement not found")
	}
	if err != nil {
		return 0, "", "", err
	}
	if agreementStatus == "CLOSED" || contractStatus == "CLOSED" {
		return 0, "", "", ErrAgreementClosedForProvision
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
		return 0, fmt.Errorf("%w: debt group %s", ErrProvisionRateMissing, debtGroupCode)
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
		SELECT COALESCE(ROUND(SUM(cc.coll_value_minor * c.deduction_ratio / 100), 0), 0)::bigint
		FROM lnm_contract_collaterals cc
		LEFT JOIN lnm_collaterals c
		  ON c.tenant_id = cc.tenant_id AND c.coll_code = cc.coll_code
		WHERE cc.tenant_id = $1 AND cc.contract_code = $2`, tenantID, contractCode).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}
