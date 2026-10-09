package repository

import (
	"context"
)

// AccruableAgreement is one ACTIVE agreement eligible for accrual.
type AccruableAgreement struct {
	ID                string
	ContractCode      string
	AgreementCode     string
	DisburseDate      string
	OutstandingAmt    int64
	InterestRate      float64
	DebtGroupCode     string
	AccClassification string
	CurrencyCode      string
}

// ListActiveAgreementsForAccrual returns ACTIVE agreements with positive
// outstanding not yet accrued to toDate.
func (r *LoanRepository) ListActiveAgreementsForAccrual(ctx context.Context, tenantID, toDate string) ([]AccruableAgreement, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.contract_code, a.agreement_code, a.disburse_date::text,
		       a.outstanding_amt_minor, a.interest_rate, a.debt_group_code,
		       COALESCE(a.acc_classification,''), COALESCE(a.currency_code,'')
		FROM lnm_agreements a
		WHERE a.tenant_id = $1 AND a.status = 'ACTIVE' AND a.outstanding_amt_minor > 0
		  AND a.disburse_date <= $2::date
		  AND NOT EXISTS (
		        SELECT 1 FROM lnm_accruals x
		        WHERE x.tenant_id = a.tenant_id AND x.agreement_code = a.agreement_code
		          AND x.to_date = $2::date AND x.status = 'POSTED')
		ORDER BY a.agreement_code`, tenantID, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccruableAgreement{}
	for rows.Next() {
		var a AccruableAgreement
		if err := rows.Scan(&a.ID, &a.ContractCode, &a.AgreementCode, &a.DisburseDate,
			&a.OutstandingAmt, &a.InterestRate, &a.DebtGroupCode, &a.AccClassification, &a.CurrencyCode); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
