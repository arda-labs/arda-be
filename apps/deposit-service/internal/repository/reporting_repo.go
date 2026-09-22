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
