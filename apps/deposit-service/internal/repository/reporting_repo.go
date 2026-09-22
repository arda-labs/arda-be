package repository

import (
	"context"
	"database/sql"
)

// ListSavingsForReporting returns every savings account of the tenant
// (optionally narrowed to one org_code) in the reporting projection. Unlike
// ListSavings it is not capped, because the statistical ETL materialises the
// whole tenant slice for one business date.
func (r *DepositRepository) ListSavingsForReporting(ctx context.Context, tenantID, orgCode string) ([]Savings, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, savings_code, customer_code, product_code, open_date::text, maturity_date::text,
		       principal_minor, accrued_minor, currency_code, COALESCE(org_code,''), status,
		       workflow_case_id::text, journal_entry_id::text, created_by, created_at, updated_at, version
		FROM dpm_savings
		WHERE tenant_id = $1 AND ($2 = '' OR org_code = $2)
		ORDER BY savings_code`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Savings{}
	for rows.Next() {
		var s Savings
		var caseID, entryID sql.NullString
		if err := rows.Scan(&s.ID, &s.TenantID, &s.SavingsCode, &s.CustomerCode, &s.ProductCode,
			&s.OpenDate, &s.MaturityDate, &s.PrincipalMinor, &s.AccruedMinor, &s.CurrencyCode,
			&s.OrgCode, &s.Status, &caseID, &entryID, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
			&s.DataVersion); err != nil {
			return nil, err
		}
		if caseID.Valid {
			s.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			s.JournalEntryID = &entryID.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// IBMReportingRow is one interbank deposit in the reporting projection: the
// business fields the PCF "Tiền gửi TCTD" indicators read, with org_code and
// the product term resolved.
type IBMReportingRow struct {
	DepositCode      string  `json:"deposit_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	ProductCode      string  `json:"product_code"`
	TermMonths       int     `json:"term_months"`
	DepositDate      string  `json:"deposit_date"`
	MaturityDate     string  `json:"maturity_date"`
	PrincipalMinor   int64   `json:"principal_minor"`
	AccruedMinor     int64   `json:"accrued_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	OrgCode          string  `json:"org_code"`
	Status           string  `json:"status"`
}

// ListIBMDepositsForReporting returns every interbank deposit of the tenant
// with its product term, for the statistical ETL. Uncapped by design (the ETL
// materialises the whole tenant slice for one business date).
func (r *DepositRepository) ListIBMDepositsForReporting(ctx context.Context, tenantID, orgCode string) ([]IBMReportingRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.deposit_code, d.counterparty_code, COALESCE(d.product_code,''),
		       COALESCE(p.term_months, 0),
		       d.deposit_date::text, d.maturity_date::text,
		       d.principal_minor, d.accrued_minor, d.interest_rate, d.currency_code,
		       COALESCE(d.org_code,''), d.status
		FROM ibm_deposits d
		LEFT JOIN ibm_products p ON p.tenant_id = d.tenant_id AND p.code = d.product_code
		WHERE d.tenant_id = $1 AND ($2 = '' OR d.org_code = $2)
		ORDER BY d.deposit_code`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IBMReportingRow{}
	for rows.Next() {
		var x IBMReportingRow
		if err := rows.Scan(&x.DepositCode, &x.CounterpartyCode, &x.ProductCode, &x.TermMonths,
			&x.DepositDate, &x.MaturityDate, &x.PrincipalMinor, &x.AccruedMinor, &x.InterestRate,
			&x.CurrencyCode, &x.OrgCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
