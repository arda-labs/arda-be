package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

type PlatformRepository struct {
	db *sql.DB
}

func NewPlatformRepository(db *sql.DB) *PlatformRepository {
	return &PlatformRepository{db: db}
}

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_fallback"
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func (r *PlatformRepository) ListParameters(ctx context.Context, tenantID, scopeType, scopeID string) ([]domain.Parameter, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, key, value, value_type, scope_type, scope_id, description, is_secret, created_at, updated_at
		FROM plt_system_parameters
		WHERE ($1 = '' OR COALESCE(tenant_id, '') = $1)
		  AND ($2 = '' OR scope_type = $2)
		  AND ($3 = '' OR COALESCE(scope_id, '') = $3)
		ORDER BY key`, tenantID, scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.Parameter, 0)
	for rows.Next() {
		var item domain.Parameter
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Key, &item.Value, &item.ValueType, &item.ScopeType, &item.ScopeID, &item.Description, &item.IsSecret, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if item.IsSecret {
			item.Value = ""
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) UpsertParameter(ctx context.Context, item domain.Parameter) (domain.Parameter, error) {
	if item.ID == "" {
		item.ID = NewID("param")
	}
	if item.ValueType == "" {
		item.ValueType = "string"
	}
	if item.ScopeType == "" {
		item.ScopeType = domain.ScopeGlobal
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_system_parameters (id, tenant_id, key, value, value_type, scope_type, scope_id, description, is_secret)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (key, scope_type, COALESCE(scope_id, ''), COALESCE(tenant_id, ''))
		DO UPDATE SET value = EXCLUDED.value, value_type = EXCLUDED.value_type, description = EXCLUDED.description,
			is_secret = EXCLUDED.is_secret, updated_at = now()
		RETURNING id, tenant_id, key, value, value_type, scope_type, scope_id, description, is_secret, created_at, updated_at`,
		item.ID, item.TenantID, item.Key, item.Value, item.ValueType, item.ScopeType, item.ScopeID, item.Description, item.IsSecret,
	).Scan(&item.ID, &item.TenantID, &item.Key, &item.Value, &item.ValueType, &item.ScopeType, &item.ScopeID, &item.Description, &item.IsSecret, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domain.Parameter{}, err
	}
	if item.IsSecret {
		item.Value = ""
	}
	return item, nil
}

func (r *PlatformRepository) ListLookupCategories(ctx context.Context, tenantID, scopeType, scopeID string) ([]domain.LookupCategory, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, scope_type, scope_id, is_system, description, created_at, updated_at
		FROM plt_lookup_categories
		WHERE ($1 = '' OR COALESCE(tenant_id, '') = $1)
		  AND ($2 = '' OR scope_type = $2)
		  AND ($3 = '' OR COALESCE(scope_id, '') = $3)
		ORDER BY code`, tenantID, scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.LookupCategory, 0)
	for rows.Next() {
		var item domain.LookupCategory
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.ScopeType, &item.ScopeID, &item.IsSystem, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) UpsertLookupCategory(ctx context.Context, item domain.LookupCategory) (domain.LookupCategory, error) {
	if item.ID == "" {
		item.ID = NewID("lookup_cat")
	}
	if item.ScopeType == "" {
		item.ScopeType = domain.ScopeGlobal
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_lookup_categories (id, tenant_id, code, name, scope_type, scope_id, is_system, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (code, scope_type, COALESCE(scope_id, ''), COALESCE(tenant_id, ''))
		DO UPDATE SET name = EXCLUDED.name, is_system = EXCLUDED.is_system, description = EXCLUDED.description, updated_at = now()
		RETURNING id, tenant_id, code, name, scope_type, scope_id, is_system, description, created_at, updated_at`,
		item.ID, item.TenantID, item.Code, item.Name, item.ScopeType, item.ScopeID, item.IsSystem, item.Description,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.ScopeType, &item.ScopeID, &item.IsSystem, &item.Description, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) ListLookupValues(ctx context.Context, categoryCode string) ([]domain.LookupValue, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.id, v.category_id, v.code, v.name, v.sort_order, v.is_active, v.metadata::text, v.created_at, v.updated_at
		FROM plt_lookup_values v
		JOIN plt_lookup_categories c ON c.id = v.category_id
		WHERE c.code = $1
		ORDER BY v.sort_order, v.name`, categoryCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.LookupValue, 0)
	for rows.Next() {
		var item domain.LookupValue
		if err := rows.Scan(&item.ID, &item.CategoryID, &item.Code, &item.Name, &item.SortOrder, &item.IsActive, &item.Metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) CreateLookupValue(ctx context.Context, categoryCode string, item domain.LookupValue) (domain.LookupValue, error) {
	if item.ID == "" {
		item.ID = NewID("lookup_val")
	}
	if !item.IsActive {
		item.IsActive = true
	}
	err := r.db.QueryRowContext(ctx, `SELECT id FROM plt_lookup_categories WHERE code = $1 LIMIT 1`, categoryCode).Scan(&item.CategoryID)
	if err != nil {
		return domain.LookupValue{}, fmt.Errorf("lookup category not found: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO plt_lookup_values (id, category_id, code, name, sort_order, is_active, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, category_id, code, name, sort_order, is_active, metadata::text, created_at, updated_at`,
		item.ID, item.CategoryID, item.Code, item.Name, item.SortOrder, item.IsActive, item.Metadata,
	).Scan(&item.ID, &item.CategoryID, &item.Code, &item.Name, &item.SortOrder, &item.IsActive, &item.Metadata, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) ListOrganizations(ctx context.Context, tenantID string) ([]domain.Organization, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, parent_id, code, name, org_type, admin_unit_code, address, is_active, created_at, updated_at
		FROM plt_organizations
		WHERE ($1 = '' OR tenant_id = $1)
		ORDER BY parent_id NULLS FIRST, code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.Organization, 0)
	for rows.Next() {
		var item domain.Organization
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.OrgType, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) CreateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	if item.ID == "" {
		item.ID = NewID("org")
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	if item.OrgType == "" {
		item.OrgType = "branch"
	}
	if !item.IsActive {
		item.IsActive = true
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_organizations (id, tenant_id, parent_id, code, name, org_type, admin_unit_code, address, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, parent_id, code, name, org_type, admin_unit_code, address, is_active, created_at, updated_at`,
		item.ID, item.TenantID, item.ParentID, item.Code, item.Name, item.OrgType, item.AdminUnitCode, item.Address, item.IsActive,
	).Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.OrgType, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) ListGeoAdminUnits(ctx context.Context, parentCode string, level int) ([]domain.GeoAdminUnit, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT code, name, full_name, parent_code, level, unit_type, country_code, region_code,
			effective_from::text, effective_to::text, is_active, metadata::text, created_at, updated_at
		FROM geo_admin_units
		WHERE ($1 = '' OR COALESCE(parent_code, '') = $1)
		  AND ($2 = 0 OR level = $2)
		  AND is_active = true
		ORDER BY level, name`, parentCode, level)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.GeoAdminUnit, 0)
	for rows.Next() {
		var item domain.GeoAdminUnit
		if err := rows.Scan(&item.Code, &item.Name, &item.FullName, &item.ParentCode, &item.Level, &item.UnitType, &item.CountryCode, &item.RegionCode, &item.EffectiveFrom, &item.EffectiveTo, &item.IsActive, &item.Metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) UpsertGeoAdminUnit(ctx context.Context, item domain.GeoAdminUnit) (domain.GeoAdminUnit, error) {
	if item.CountryCode == "" {
		item.CountryCode = "VN"
	}
	if item.EffectiveFrom == "" {
		item.EffectiveFrom = "2025-07-01"
	}
	if !item.IsActive {
		item.IsActive = true
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO geo_admin_units (code, name, full_name, parent_code, level, unit_type, country_code, region_code, effective_from, effective_to, is_active, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::date, $10::date, $11, $12)
		ON CONFLICT (code)
		DO UPDATE SET name = EXCLUDED.name, full_name = EXCLUDED.full_name, parent_code = EXCLUDED.parent_code,
			level = EXCLUDED.level, unit_type = EXCLUDED.unit_type, country_code = EXCLUDED.country_code,
			region_code = EXCLUDED.region_code, effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to, is_active = EXCLUDED.is_active,
			metadata = EXCLUDED.metadata, updated_at = now()
		RETURNING code, name, full_name, parent_code, level, unit_type, country_code, region_code,
			effective_from::text, effective_to::text, is_active, metadata::text, created_at, updated_at`,
		item.Code, item.Name, item.FullName, item.ParentCode, item.Level, item.UnitType, item.CountryCode, item.RegionCode, item.EffectiveFrom, item.EffectiveTo, item.IsActive, item.Metadata,
	).Scan(&item.Code, &item.Name, &item.FullName, &item.ParentCode, &item.Level, &item.UnitType, &item.CountryCode, &item.RegionCode, &item.EffectiveFrom, &item.EffectiveTo, &item.IsActive, &item.Metadata, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}
