package repository

import (
	"context"
	"fmt"
	"strings"
)

// CustomerReportRow is one báo cáo khách hàng row (W4c).
type CustomerReportRow struct {
	CustomerCode string `json:"customer_code"`
	Name         string `json:"name"`
	CustomerType string `json:"customer_type"`
	Mobile       string `json:"mobile,omitempty"`
	Segment      string `json:"segment,omitempty"`
	CustomerRank string `json:"customer_rank,omitempty"`
	RiskLevel    string `json:"risk_level,omitempty"`
	Status       string `json:"status"`
	OrgID        string `json:"org_id,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

// CustomerReport lists customers filtered by q (code/name/mobile) and type.
func (r *CustomerRepository) CustomerReport(ctx context.Context, tenantID, q, customerType, status string) ([]CustomerReportRow, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(customer_code ILIKE $%d OR name ILIKE $%d OR mobile ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	if customerType != "" {
		args = append(args, customerType)
		where = append(where, fmt.Sprintf("customer_type = $%d::text", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT customer_code, name, customer_type, COALESCE(mobile,''), COALESCE(segment,''),
		       COALESCE(customer_rank,''), COALESCE(risk_level,''), status, COALESCE(org_id,''),
		       created_at::text
		FROM customers WHERE `+strings.Join(where, " AND ")+`
		ORDER BY customer_code LIMIT 2000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerReportRow{}
	for rows.Next() {
		var x CustomerReportRow
		if err := rows.Scan(&x.CustomerCode, &x.Name, &x.CustomerType, &x.Mobile, &x.Segment,
			&x.CustomerRank, &x.RiskLevel, &x.Status, &x.OrgID, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
