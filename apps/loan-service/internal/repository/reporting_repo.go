package repository

import (
	"context"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// ListAgreementsForReporting returns every drawdown of the tenant (optionally
// narrowed to one org_code) in the reporting projection. Unlike ListAgreements
// it is not paged and carries org_code + product_code, because the statistical
// ETL must materialise the whole tenant slice for one business date.
func (r *LoanRepository) ListAgreementsForReporting(ctx context.Context, tenantID, orgCode string) ([]domain.ReportingAgreement, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.agreement_code, a.contract_code, COALESCE(c.customer_code, ''),
		       COALESCE(c.product_code, ''), COALESCE(a.org_code, ''),
		       a.disburse_date::text, a.maturity_date::text, a.debt_group_code,
		       a.status, a.currency_code, a.interest_rate, a.disburse_amt_minor,
		       a.outstanding_amt_minor, a.provision_amt_minor
		FROM lnm_agreements a
		LEFT JOIN lnm_contracts c ON c.tenant_id = a.tenant_id AND c.contract_code = a.contract_code
		WHERE a.tenant_id = $1 AND ($2 = '' OR a.org_code = $2)
		ORDER BY a.agreement_code`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ReportingAgreement{}
	for rows.Next() {
		var item domain.ReportingAgreement
		if err := rows.Scan(&item.AgreementCode, &item.ContractCode, &item.CustomerCode,
			&item.ProductCode, &item.OrgCode, &item.DisburseDate, &item.MaturityDate,
			&item.DebtGroupCode, &item.Status, &item.CurrencyCode, &item.InterestRate,
			&item.DisburseAmtMinor, &item.OutstandingAmtMinor, &item.ProvisionAmtMinor); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListCollateralsForReporting returns every collateral of the tenant in the
// reporting projection. Collateral rows carry no org_code in the domain model
// (org_scope only stamped the contract/agreement tables), so org_code is
// reported empty until the domain adds it.
func (r *LoanRepository) ListCollateralsForReporting(ctx context.Context, tenantID string) ([]domain.ReportingCollateral, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT coll_code, COALESCE(coll_type_code, ''), '' AS org_code,
		       valuation_date::text, status, coll_value_minor, coll_use_value_minor
		FROM lnm_collaterals
		WHERE tenant_id = $1
		ORDER BY coll_code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ReportingCollateral{}
	for rows.Next() {
		var item domain.ReportingCollateral
		if err := rows.Scan(&item.CollCode, &item.CollTypeCode, &item.OrgCode,
			&item.ValuationDate, &item.Status, &item.CollValueMinor, &item.CollUseValueMinor); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
