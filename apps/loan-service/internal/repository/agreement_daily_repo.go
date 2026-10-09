package repository

import (
	"context"
	"time"
)

// AgreementDailySnapshot is the exact end-of-day balance snapshot for one
// agreement and business date.
type AgreementDailySnapshot struct {
	TenantID                string    `json:"tenant_id"`
	AgreementID             string    `json:"agreement_id"`
	DataDate                string    `json:"data_date"`
	CurrencyCode            string    `json:"currency_code"`
	DisburseAmtMinor        int64     `json:"disburse_amt_minor"`
	PendingDisburseAmtMinor int64     `json:"pending_disburse_amt_minor"`
	OutstandingAmtMinor     int64     `json:"outstanding_amt_minor"`
	ColnPrincipalAmtMinor   int64     `json:"coln_principal_amt_minor"`
	ColnInterestAmtMinor    int64     `json:"coln_interest_amt_minor"`
	ProvisionAmtMinor       int64     `json:"provision_amt_minor"`
	OffBalPrincipalMinor    int64     `json:"off_bal_principal_minor"`
	OffBalInterestMinor     int64     `json:"off_bal_interest_minor"`
	OffBalDueInterestMinor  int64     `json:"off_bal_due_interest_minor"`
	CapturedAt              time.Time `json:"captured_at"`
}

// CaptureAgreementDailySnapshots upserts one end-of-day snapshot for every
// agreement owned by tenantID. Repeating the same business date is idempotent.
func (r *LoanRepository) CaptureAgreementDailySnapshots(ctx context.Context, tenantID, dataDate string) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO lnm_agreement_daily (
			tenant_id, agreement_id, data_date, currency_code,
			disburse_amt_minor, pending_disburse_amt_minor, outstanding_amt_minor,
			coln_principal_amt_minor, coln_interest_amt_minor, provision_amt_minor,
			off_bal_principal_minor, off_bal_interest_minor, off_bal_due_interest_minor
		)
		SELECT a.tenant_id, a.id, $2::date, a.currency_code,
		       a.disburse_amt_minor, a.pending_disburse_amt_minor, a.outstanding_amt_minor,
		       a.coln_principal_amt_minor, a.coln_interest_amt_minor, a.provision_amt_minor,
		       a.off_bal_principal_minor, a.off_bal_interest_minor, a.off_bal_due_interest_minor
		FROM lnm_agreements a
		WHERE a.tenant_id = $1
		ON CONFLICT (agreement_id, data_date) DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			currency_code = EXCLUDED.currency_code,
			disburse_amt_minor = EXCLUDED.disburse_amt_minor,
			pending_disburse_amt_minor = EXCLUDED.pending_disburse_amt_minor,
			outstanding_amt_minor = EXCLUDED.outstanding_amt_minor,
			coln_principal_amt_minor = EXCLUDED.coln_principal_amt_minor,
			coln_interest_amt_minor = EXCLUDED.coln_interest_amt_minor,
			provision_amt_minor = EXCLUDED.provision_amt_minor,
			off_bal_principal_minor = EXCLUDED.off_bal_principal_minor,
			off_bal_interest_minor = EXCLUDED.off_bal_interest_minor,
			off_bal_due_interest_minor = EXCLUDED.off_bal_due_interest_minor,
			captured_at = now()`, tenantID, dataDate)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ListAgreementDailySnapshots returns only snapshots captured for dataDate;
// it intentionally does not substitute a prior day's row when one is missing.
func (r *LoanRepository) ListAgreementDailySnapshots(ctx context.Context, tenantID, dataDate string) ([]AgreementDailySnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tenant_id, agreement_id, data_date::text, currency_code,
		       disburse_amt_minor, pending_disburse_amt_minor, outstanding_amt_minor,
		       coln_principal_amt_minor, coln_interest_amt_minor, provision_amt_minor,
		       off_bal_principal_minor, off_bal_interest_minor, off_bal_due_interest_minor,
		       captured_at
		FROM lnm_agreement_daily
		WHERE tenant_id = $1 AND data_date = $2::date
		ORDER BY agreement_id`, tenantID, dataDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshots := make([]AgreementDailySnapshot, 0)
	for rows.Next() {
		var snapshot AgreementDailySnapshot
		if err := rows.Scan(&snapshot.TenantID, &snapshot.AgreementID, &snapshot.DataDate, &snapshot.CurrencyCode,
			&snapshot.DisburseAmtMinor, &snapshot.PendingDisburseAmtMinor, &snapshot.OutstandingAmtMinor,
			&snapshot.ColnPrincipalAmtMinor, &snapshot.ColnInterestAmtMinor, &snapshot.ProvisionAmtMinor,
			&snapshot.OffBalPrincipalMinor, &snapshot.OffBalInterestMinor, &snapshot.OffBalDueInterestMinor,
			&snapshot.CapturedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}
