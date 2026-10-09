package repository

import (
	"context"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

const productColumns = `id, tenant_id, code, name, product_type, currency_code, interest_rate_code,
	interest_rate, loan_term_from, loan_term_to, term_unit, min_amount_minor, max_amount_minor,
	acc_classification, is_active, description, created_by, created_at, updated_at`

func scanProduct(s interface{ Scan(...any) error }) (domain.LoanProduct, error) {
	var p domain.LoanProduct
	err := s.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.ProductType, &p.CurrencyCode,
		&p.InterestRateCode, &p.InterestRate, &p.LoanTermFrom, &p.LoanTermTo, &p.TermUnit,
		&p.MinAmount, &p.MaxAmount, &p.AccClassification, &p.IsActive, &p.Description,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// productOrderBy maps the whitelisted sort field for the product catalog;
// the historical default is code ASC.
func productOrderBy(sort, order string) string {
	col := "code"
	switch sort {
	case "name":
		col = "name"
	case "created_at":
		col = "created_at"
	}
	if sort != "" && order == "desc" {
		return col + " DESC"
	}
	return col + " ASC"
}

func (r *LoanRepository) ListProducts(ctx context.Context, tenantID string, includeInactive bool, isActive string, q, sort, order string) ([]domain.LoanProduct, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+productColumns+`
		FROM lnm_products
		WHERE tenant_id = $1
		  AND ($2 OR is_active)
		  AND ($3::text = '' OR is_active::text = ANY(string_to_array($3::text, ',')))
		  AND ($4::text = '' OR code ILIKE '%' || $4 || '%' OR name ILIKE '%' || $4 || '%')
		ORDER BY `+productOrderBy(sort, order), tenantID, includeInactive, isActive, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.LoanProduct{}
	for rows.Next() {
		item, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *LoanRepository) GetProductByCode(ctx context.Context, tenantID, code string) (domain.LoanProduct, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+productColumns+` FROM lnm_products WHERE tenant_id = $1 AND code = $2 AND is_active`, tenantID, code)
	item, err := scanProduct(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) UpsertProduct(ctx context.Context, p *domain.LoanProduct) (*domain.LoanProduct, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_products (id, tenant_id, code, name, product_type, currency_code, interest_rate_code,
			interest_rate, loan_term_from, loan_term_to, term_unit, min_amount_minor, max_amount_minor,
			acc_classification, is_active, description, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
			name = EXCLUDED.name, product_type = EXCLUDED.product_type, currency_code = EXCLUDED.currency_code,
			interest_rate_code = EXCLUDED.interest_rate_code, interest_rate = EXCLUDED.interest_rate,
			loan_term_from = EXCLUDED.loan_term_from, loan_term_to = EXCLUDED.loan_term_to,
			term_unit = EXCLUDED.term_unit, min_amount_minor = EXCLUDED.min_amount_minor, max_amount_minor = EXCLUDED.max_amount_minor,
			acc_classification = EXCLUDED.acc_classification, is_active = EXCLUDED.is_active,
			description = EXCLUDED.description, updated_at = now()
		RETURNING `+productColumns,
		p.ID, p.TenantID, p.Code, p.Name, p.ProductType, p.CurrencyCode, p.InterestRateCode,
		p.InterestRate, p.LoanTermFrom, p.LoanTermTo, p.TermUnit, p.MinAmount, p.MaxAmount,
		p.AccClassification, p.IsActive, p.Description, p.CreatedBy)
	out, err := scanProduct(row)
	return &out, err
}
