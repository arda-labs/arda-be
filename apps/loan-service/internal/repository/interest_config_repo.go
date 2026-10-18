package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/shopspring/decimal"
)

// ListInterestConfigVersions loads every stored interest-configuration version
// of one product that can apply to orgCode: that org's versions and the
// tenant-wide ones (org_code NULL). orgCode "" loads the tenant-wide versions
// only. Which version is in force on a day, and whether it is usable, is decided
// by domain.ResolveInterestConfig so the whole rule is unit-testable without a
// database; the number of versions per product is small, so no date filter is
// pushed into SQL.
func (r *LoanRepository) ListInterestConfigVersions(ctx context.Context, tenantID, productCode, orgCode string) ([]domain.InterestConfigRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, product_code, org_code, to_char(effective_from, 'YYYY-MM-DD'),
		       interest_base_code, interest_calc_method, interest_date_method, interest_start_method,
		       overdue_check_cond, round_no, round_type, principal_grace_days,
		       overdue_rate_type, overdue_rate_value::text
		FROM lnm_product_interest_config
		WHERE tenant_id = $1 AND product_code = $2
		  AND (org_code IS NULL OR ($3::text <> '' AND org_code = $3))
		ORDER BY effective_from, org_code NULLS FIRST`, tenantID, productCode, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InterestConfigRecord
	for rows.Next() {
		var (
			rec         domain.InterestConfigRecord
			org         sql.NullString
			grace       sql.NullInt64
			overdueType sql.NullString
			overdueVal  sql.NullString
		)
		if err := rows.Scan(&rec.ID, &rec.TenantID, &rec.ProductCode, &org, &rec.EffectiveFrom,
			&rec.InterestBaseCode, &rec.InterestCalcMethod, &rec.InterestDateMethod, &rec.InterestStartMethod,
			&rec.OverdueCheckCond, &rec.RoundNo, &rec.RoundType, &grace, &overdueType, &overdueVal); err != nil {
			return nil, err
		}
		if org.Valid {
			rec.OrgCode = &org.String
		}
		if grace.Valid {
			g := int(grace.Int64)
			rec.PrincipalGraceDays = &g
		}
		if overdueType.Valid {
			rec.OverdueRateType = &overdueType.String
		}
		if overdueVal.Valid {
			v, err := decimal.NewFromString(overdueVal.String)
			if err != nil {
				return nil, fmt.Errorf("interest config %s: overdue_rate_value %q: %w", rec.ID, overdueVal.String, err)
			}
			rec.OverdueRateValue = &v
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
