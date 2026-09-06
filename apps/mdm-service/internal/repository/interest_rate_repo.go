package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
)

type InterestRateRepository struct {
	db *sql.DB
}

func NewInterestRateRepository(db *sql.DB) *InterestRateRepository {
	return &InterestRateRepository{db: db}
}

const rateColumns = `id, tenant_id, code, name, rate_type, apply_type, currency_code, description, is_active, created_at, updated_at`

func scanRate(scanner interface{ Scan(...any) error }) (domain.InterestRate, error) {
	var item domain.InterestRate
	err := scanner.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.RateType, &item.ApplyType, &item.CurrencyCode, &item.Description, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *InterestRateRepository) List(ctx context.Context, tenantID, q string, includeInactive bool) ([]domain.InterestRate, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM mdm_interest_rates
		WHERE (tenant_id IS NULL OR tenant_id = $1)
		  AND ($2 OR is_active)
		  AND ($3 = '' OR code ILIKE '%%' || $3 || '%%' OR name ILIKE '%%' || $3 || '%%')
		ORDER BY code`, rateColumns), tenantID, includeInactive, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.InterestRate, 0)
	for rows.Next() {
		item, err := scanRate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *InterestRateRepository) Get(ctx context.Context, tenantID, id string) (domain.InterestRate, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM mdm_interest_rates
		WHERE id = $1 AND (tenant_id IS NULL OR tenant_id = $2)`, rateColumns), id, tenantID)
	item, err := scanRate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InterestRate{}, fmt.Errorf("%w", ErrNotFound)
	}
	return item, err
}

func (r *InterestRateRepository) Create(ctx context.Context, item domain.InterestRate) (domain.InterestRate, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		INSERT INTO mdm_interest_rates (id, tenant_id, code, name, rate_type, apply_type, currency_code, description, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING
		RETURNING %s`, rateColumns),
		item.ID, item.TenantID, item.Code, item.Name, item.RateType, item.ApplyType, item.CurrencyCode, item.Description, item.IsActive)
	created, err := scanRate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InterestRate{}, fmt.Errorf("%w", ErrConflict)
	}
	return created, err
}

func (r *InterestRateRepository) Update(ctx context.Context, item domain.InterestRate) (domain.InterestRate, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE mdm_interest_rates
		SET code = $3, name = $4, rate_type = $5, apply_type = $6, currency_code = $7, description = $8, is_active = $9, updated_at = now()
		WHERE id = $1 AND (tenant_id IS NULL OR tenant_id = $2)
		RETURNING %s`, rateColumns),
		item.ID, item.TenantID, item.Code, item.Name, item.RateType, item.ApplyType, item.CurrencyCode, item.Description, item.IsActive)
	updated, err := scanRate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InterestRate{}, fmt.Errorf("%w", ErrNotFound)
	}
	return updated, err
}

func (r *InterestRateRepository) Delete(ctx context.Context, tenantID, id string) error {
	// Tiers are owned by the rate header; remove them first so no orphan
	// validity windows survive a header delete.
	if _, err := r.db.ExecContext(ctx, `DELETE FROM mdm_interest_rate_tiers WHERE rate_id = $1`, id); err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM mdm_interest_rates WHERE id = $1 AND (tenant_id IS NULL OR tenant_id = $2)`, id, tenantID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

const tierColumns = `id, rate_id, effective_from::text, effective_to::text, amount_from_minor, amount_to_minor, rate_value, min_rate, max_rate, decision_no, decision_date::text, created_at, updated_at`

func scanTier(scanner interface{ Scan(...any) error }) (domain.InterestRateTier, error) {
	var item domain.InterestRateTier
	err := scanner.Scan(&item.ID, &item.RateID, &item.EffectiveFrom, &item.EffectiveTo, &item.AmountFrom, &item.AmountTo, &item.RateValue, &item.MinRate, &item.MaxRate, &item.DecisionNo, &item.DecisionDate, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *InterestRateRepository) ListTiers(ctx context.Context, tenantID, rateID string) ([]domain.InterestRateTier, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.%s
		FROM mdm_interest_rate_tiers t
		JOIN mdm_interest_rates h ON h.id = t.rate_id
		WHERE t.rate_id = $1 AND (h.tenant_id IS NULL OR h.tenant_id = $2)
		ORDER BY t.effective_from DESC, t.amount_from_minor NULLS FIRST`, tierColumns), rateID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.InterestRateTier, 0)
	for rows.Next() {
		item, err := scanTier(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *InterestRateRepository) GetTier(ctx context.Context, tenantID, rateID, tierID string) (domain.InterestRateTier, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT t.%s
		FROM mdm_interest_rate_tiers t
		JOIN mdm_interest_rates h ON h.id = t.rate_id
		WHERE t.id = $1 AND t.rate_id = $2 AND (h.tenant_id IS NULL OR h.tenant_id = $3)`, tierColumns), tierID, rateID, tenantID)
	item, err := scanTier(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InterestRateTier{}, fmt.Errorf("%w", ErrNotFound)
	}
	return item, err
}

func (r *InterestRateRepository) CreateTier(ctx context.Context, tenantID, rateID string, tier domain.InterestRateTier) (domain.InterestRateTier, error) {
	if _, err := r.Get(ctx, tenantID, rateID); err != nil {
		return domain.InterestRateTier{}, err
	}
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		INSERT INTO mdm_interest_rate_tiers (id, rate_id, effective_from, effective_to, amount_from_minor, amount_to_minor, rate_value, min_rate, max_rate, decision_no, decision_date)
		VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8, $9, $10, $11::date)
		RETURNING %s`, tierColumns),
		tier.ID, rateID, tier.EffectiveFrom, tier.EffectiveTo, tier.AmountFrom, tier.AmountTo, tier.RateValue, tier.MinRate, tier.MaxRate, tier.DecisionNo, tier.DecisionDate)
	return scanTier(row)
}

func (r *InterestRateRepository) UpdateTier(ctx context.Context, tenantID, rateID, tierID string, tier domain.InterestRateTier) (domain.InterestRateTier, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE mdm_interest_rate_tiers t
		SET effective_from = $3::date, effective_to = $4::date, amount_from_minor = $5, amount_to_minor = $6,
		    rate_value = $7, min_rate = $8, max_rate = $9, decision_no = $10, decision_date = $11::date, updated_at = now()
		FROM mdm_interest_rates h
		WHERE h.id = t.rate_id AND t.id = $1 AND t.rate_id = $2 AND (h.tenant_id IS NULL OR h.tenant_id = $12)
		RETURNING t.%s`, tierColumns),
		tierID, rateID, tier.EffectiveFrom, tier.EffectiveTo, tier.AmountFrom, tier.AmountTo, tier.RateValue, tier.MinRate, tier.MaxRate, tier.DecisionNo, tier.DecisionDate, tenantID)
	updated, err := scanTier(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InterestRateTier{}, fmt.Errorf("%w", ErrNotFound)
	}
	return updated, err
}

func (r *InterestRateRepository) DeleteTier(ctx context.Context, tenantID, rateID, tierID string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM mdm_interest_rate_tiers t
		USING mdm_interest_rates h
		WHERE h.id = t.rate_id AND t.id = $1 AND t.rate_id = $2 AND (h.tenant_id IS NULL OR h.tenant_id = $3)`,
		tierID, rateID, tenantID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}
