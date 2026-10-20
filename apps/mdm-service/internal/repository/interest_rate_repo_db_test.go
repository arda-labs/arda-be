package repository

// AP-TEN-02: bảng lãi toàn cục (tenant_id IS NULL) dùng chung cho mọi tenant.
// Tenant có quyền mdm-write chỉ được đọc tier của nó, không được tạo/sửa/xóa;
// hàng toàn cục chỉ đổi qua migration.

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	"github.com/arda-labs/arda/apps/mdm-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func openMDMTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
}

func TestTenantScopeGlobalTierIsReadOnlyForTenants(t *testing.T) {
	ctx := context.Background()
	db := openMDMTestDB(t)
	repo := NewInterestRateRepository(db)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO mdm_interest_rates (id, tenant_id, code, name, rate_type, apply_type)
		VALUES ('rate_global', NULL, 'GLOBAL', 'Lãi toàn cục', 'central', 'by_term');
		INSERT INTO mdm_interest_rate_tiers (id, rate_id, effective_from, rate_value)
		VALUES ('tier_global', 'rate_global', DATE '2026-01-01', 4.5)`); err != nil {
		t.Fatalf("seed global rate: %v", err)
	}

	// Đọc vẫn được: tenant thấy tier toàn cục.
	tiers, err := repo.ListTiers(ctx, "tenant-b", "rate_global")
	if err != nil || len(tiers) != 1 {
		t.Fatalf("ListTiers global = %d tiers, err %v; want 1 tier", len(tiers), err)
	}
	if _, err := repo.GetTier(ctx, "tenant-b", "rate_global", "tier_global"); err != nil {
		t.Fatalf("GetTier global: %v", err)
	}

	changed := domain.InterestRateTier{EffectiveFrom: "2026-01-01", RateValue: 99}
	if _, err := repo.UpdateTier(ctx, "tenant-b", "rate_global", "tier_global", changed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateTier global by tenant-b err = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteTier(ctx, "tenant-b", "rate_global", "tier_global"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteTier global by tenant-b err = %v, want ErrNotFound", err)
	}
	created := domain.InterestRateTier{ID: "tier_injected", EffectiveFrom: "2027-01-01", RateValue: 1}
	if _, err := repo.CreateTier(ctx, "tenant-b", "rate_global", created); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateTier on global rate by tenant-b err = %v, want ErrNotFound", err)
	}

	var rateValue float64
	var count int
	if err := db.QueryRowContext(ctx, `SELECT rate_value::float8 FROM mdm_interest_rate_tiers WHERE id = 'tier_global'`).Scan(&rateValue); err != nil {
		t.Fatalf("global tier must survive: %v", err)
	}
	if rateValue != 4.5 {
		t.Fatalf("global tier rate_value = %v, want 4.5 (unchanged)", rateValue)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mdm_interest_rate_tiers WHERE rate_id = 'rate_global'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("global rate tiers = %d (err %v), want 1", count, err)
	}
}

func TestTenantScopeOwnTierWritesAndCrossTenantIsolation(t *testing.T) {
	ctx := context.Background()
	db := openMDMTestDB(t)
	repo := NewInterestRateRepository(db)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO mdm_interest_rates (id, tenant_id, code, name, rate_type, apply_type)
		VALUES ('rate_a', 'tenant-a', 'OWN', 'Lãi của A', 'deposit', 'by_term')`); err != nil {
		t.Fatalf("seed tenant rate: %v", err)
	}
	minRate := 1.25
	tier, err := repo.CreateTier(ctx, "tenant-a", "rate_a", domain.InterestRateTier{ID: "tier_a", EffectiveFrom: "2026-01-01", RateValue: 5, MinRate: &minRate})
	if err != nil {
		t.Fatalf("CreateTier own rate: %v", err)
	}
	if tier.RateID != "rate_a" || tier.RateValue != 5 {
		t.Fatalf("created tier = %+v", tier)
	}

	if _, err := repo.UpdateTier(ctx, "tenant-b", "rate_a", "tier_a", domain.InterestRateTier{EffectiveFrom: "2026-01-01", RateValue: 7}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateTier by other tenant err = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteTier(ctx, "tenant-b", "rate_a", "tier_a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteTier by other tenant err = %v, want ErrNotFound", err)
	}

	updated, err := repo.UpdateTier(ctx, "tenant-a", "rate_a", "tier_a", domain.InterestRateTier{EffectiveFrom: "2026-01-01", RateValue: 6})
	if err != nil || updated.RateValue != 6 {
		t.Fatalf("UpdateTier own = %+v, err %v", updated, err)
	}
	if err := repo.DeleteTier(ctx, "tenant-a", "rate_a", "tier_a"); err != nil {
		t.Fatalf("DeleteTier own: %v", err)
	}
}
