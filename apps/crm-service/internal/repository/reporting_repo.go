package repository

import (
	"context"
)

// ReportingCustomer is the reporting read-model projection of one customer.
// PII (name, email, mobile, identity, address) is intentionally excluded —
// reports aggregate by segment/type/status, never by person.
type ReportingCustomer struct {
	CustomerCode string `json:"customer_code"`
	OrgCode      string `json:"org_code"`
	CustomerType string `json:"customer_type"`
	Status       string `json:"status"`
	Segment      string `json:"segment"`
	CustomerRank string `json:"customer_rank"`
	RiskLevel    string `json:"risk_level"`
}

// ListCustomersForReporting returns every customer of the tenant (optionally
// narrowed to one org_id) in the reporting projection, un-paged for the ETL.
func (r *CustomerRepository) ListCustomersForReporting(ctx context.Context, tenantID, orgID string) ([]ReportingCustomer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT customer_code, COALESCE(org_id,''), customer_type, status,
		       segment, customer_rank, risk_level
		FROM customers
		WHERE tenant_id = $1 AND ($2 = '' OR org_id = $2)
		ORDER BY customer_code`, tenantID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportingCustomer{}
	for rows.Next() {
		var x ReportingCustomer
		if err := rows.Scan(&x.CustomerCode, &x.OrgCode, &x.CustomerType, &x.Status,
			&x.Segment, &x.CustomerRank, &x.RiskLevel); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
