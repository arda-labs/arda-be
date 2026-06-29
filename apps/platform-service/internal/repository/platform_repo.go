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

func (r *PlatformRepository) GetParameter(ctx context.Context, tenantID, key, scopeType, scopeID string) (domain.Parameter, error) {
	var item domain.Parameter
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, key, value, value_type, scope_type, scope_id, description, is_secret, created_at, updated_at
		FROM plt_system_parameters
		WHERE key = $1
		  AND ($2 = '' OR COALESCE(tenant_id, '') = $2)
		  AND scope_type = $3
		  AND COALESCE(scope_id, '') = $4
		LIMIT 1`, key, tenantID, scopeType, scopeID).
		Scan(&item.ID, &item.TenantID, &item.Key, &item.Value, &item.ValueType, &item.ScopeType, &item.ScopeID, &item.Description, &item.IsSecret, &item.CreatedAt, &item.UpdatedAt)
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

func (r *PlatformRepository) UpsertLookupValue(ctx context.Context, categoryCode string, item domain.LookupValue) (domain.LookupValue, error) {
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
		ON CONFLICT (category_id, code)
		DO UPDATE SET name = EXCLUDED.name, sort_order = EXCLUDED.sort_order, is_active = EXCLUDED.is_active,
			metadata = EXCLUDED.metadata, updated_at = now()
		RETURNING id, category_id, code, name, sort_order, is_active, metadata::text, created_at, updated_at`,
		item.ID, item.CategoryID, item.Code, item.Name, item.SortOrder, item.IsActive, item.Metadata,
	).Scan(&item.ID, &item.CategoryID, &item.Code, &item.Name, &item.SortOrder, &item.IsActive, &item.Metadata, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) ListOrganizations(ctx context.Context, tenantID string) ([]domain.Organization, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, parent_id, code, name, admin_unit_code, address, is_active, created_at, updated_at
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
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
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
	if !item.IsActive {
		item.IsActive = true
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_organizations (id, tenant_id, parent_id, code, name, admin_unit_code, address, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, tenant_id, parent_id, code, name, admin_unit_code, address, is_active, created_at, updated_at`,
		item.ID, item.TenantID, item.ParentID, item.Code, item.Name, item.AdminUnitCode, item.Address, item.IsActive,
	).Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
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

func (r *PlatformRepository) GetOrganizationByID(ctx context.Context, id string) (domain.Organization, error) {
	var item domain.Organization
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, parent_id, code, name, admin_unit_code, address, is_active, created_at, updated_at
		FROM plt_organizations
		WHERE id = $1 LIMIT 1`, id).
		Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) UpdateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_organizations
		SET parent_id = $2, code = $3, name = $4, admin_unit_code = $5, address = $6, is_active = $7, updated_at = now()
		WHERE id = $1
		RETURNING id, tenant_id, parent_id, code, name, admin_unit_code, address, is_active, created_at, updated_at`,
		item.ID, item.ParentID, item.Code, item.Name, item.AdminUnitCode, item.Address, item.IsActive,
	).Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) DeleteOrganization(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE plt_organizations SET is_active = false, updated_at = now() WHERE id = $1`, id)
	return err
}

func (r *PlatformRepository) DeleteParameter(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_system_parameters WHERE id = $1`, id)
	return err
}

func (r *PlatformRepository) DeleteLookupCategory(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_lookup_categories WHERE id = $1`, id)
	return err
}

func (r *PlatformRepository) DeleteLookupValue(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_lookup_values WHERE id = $1`, id)
	return err
}

func (r *PlatformRepository) ListCreditInstitutions(ctx context.Context, tenantID, status, query string) ([]domain.CreditInstitution, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, address, status, effective_from::text, short_name, phone, email,
			license_no, license_date::text, tax_code, website, note, created_at, updated_at
		FROM plt_credit_institutions
		WHERE ($1 = '' OR tenant_id = $1)
		  AND ($2 = '' OR status = $2)
		  AND (
			$3 = ''
			OR code ILIKE '%' || $3 || '%'
			OR name ILIKE '%' || $3 || '%'
			OR COALESCE(short_name, '') ILIKE '%' || $3 || '%'
			OR COALESCE(tax_code, '') ILIKE '%' || $3 || '%'
			OR COALESCE(license_no, '') ILIKE '%' || $3 || '%'
		  )
		ORDER BY code`, tenantID, status, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.CreditInstitution, 0)
	for rows.Next() {
		var item domain.CreditInstitution
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.Code,
			&item.Name,
			&item.Address,
			&item.Status,
			&item.EffectiveFrom,
			&item.ShortName,
			&item.Phone,
			&item.Email,
			&item.LicenseNo,
			&item.LicenseDate,
			&item.TaxCode,
			&item.Website,
			&item.Note,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) GetCreditInstitutionByID(ctx context.Context, id string) (domain.CreditInstitution, error) {
	var item domain.CreditInstitution
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, address, status, effective_from::text, short_name, phone, email,
			license_no, license_date::text, tax_code, website, note, created_at, updated_at
		FROM plt_credit_institutions
		WHERE id = $1
		LIMIT 1`, id).
		Scan(
			&item.ID,
			&item.TenantID,
			&item.Code,
			&item.Name,
			&item.Address,
			&item.Status,
			&item.EffectiveFrom,
			&item.ShortName,
			&item.Phone,
			&item.Email,
			&item.LicenseNo,
			&item.LicenseDate,
			&item.TaxCode,
			&item.Website,
			&item.Note,
			&item.CreatedAt,
			&item.UpdatedAt,
		)
	return item, err
}

func (r *PlatformRepository) CreateCreditInstitution(ctx context.Context, item domain.CreditInstitution) (domain.CreditInstitution, error) {
	if item.ID == "" {
		item.ID = NewID("ci")
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_credit_institutions (
			id, tenant_id, code, name, address, status, effective_from, short_name, phone, email,
			license_no, license_date, tax_code, website, note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9, $10, $11, $12::date, $13, $14, $15)
		RETURNING id, tenant_id, code, name, address, status, effective_from::text, short_name, phone, email,
			license_no, license_date::text, tax_code, website, note, created_at, updated_at`,
		item.ID, item.TenantID, item.Code, item.Name, item.Address, item.Status, item.EffectiveFrom, item.ShortName, item.Phone, item.Email,
		item.LicenseNo, item.LicenseDate, item.TaxCode, item.Website, item.Note,
	).Scan(
		&item.ID,
		&item.TenantID,
		&item.Code,
		&item.Name,
		&item.Address,
		&item.Status,
		&item.EffectiveFrom,
		&item.ShortName,
		&item.Phone,
		&item.Email,
		&item.LicenseNo,
		&item.LicenseDate,
		&item.TaxCode,
		&item.Website,
		&item.Note,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *PlatformRepository) UpdateCreditInstitution(ctx context.Context, item domain.CreditInstitution) (domain.CreditInstitution, error) {
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_credit_institutions
		SET code = $2, name = $3, address = $4, status = $5, effective_from = $6::date, short_name = $7, phone = $8, email = $9,
			license_no = $10, license_date = $11::date, tax_code = $12, website = $13, note = $14, updated_at = now()
		WHERE id = $1
		RETURNING id, tenant_id, code, name, address, status, effective_from::text, short_name, phone, email,
			license_no, license_date::text, tax_code, website, note, created_at, updated_at`,
		item.ID, item.Code, item.Name, item.Address, item.Status, item.EffectiveFrom, item.ShortName, item.Phone, item.Email,
		item.LicenseNo, item.LicenseDate, item.TaxCode, item.Website, item.Note,
	).Scan(
		&item.ID,
		&item.TenantID,
		&item.Code,
		&item.Name,
		&item.Address,
		&item.Status,
		&item.EffectiveFrom,
		&item.ShortName,
		&item.Phone,
		&item.Email,
		&item.LicenseNo,
		&item.LicenseDate,
		&item.TaxCode,
		&item.Website,
		&item.Note,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *PlatformRepository) DeleteCreditInstitution(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_credit_institutions WHERE id = $1`, id)
	return err
}

func (r *PlatformRepository) ListAreas(ctx context.Context, tenantID, status, areaTypeCode, parentID, query string) ([]domain.Area, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, parent_id, code, name, area_type_code, admin_unit_code, description, status,
			effective_from::text, effective_to::text, created_at, updated_at
		FROM plt_areas
		WHERE ($1 = '' OR tenant_id = $1)
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR area_type_code = $3)
		  AND ($4 = '' OR COALESCE(parent_id, '') = $4)
		  AND (
			$5 = ''
			OR code ILIKE '%' || $5 || '%'
			OR name ILIKE '%' || $5 || '%'
			OR COALESCE(description, '') ILIKE '%' || $5 || '%'
		  )
		ORDER BY parent_id NULLS FIRST, code`, tenantID, status, areaTypeCode, parentID, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.Area, 0)
	for rows.Next() {
		var item domain.Area
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.ParentID,
			&item.Code,
			&item.Name,
			&item.AreaTypeCode,
			&item.AdminUnitCode,
			&item.Description,
			&item.Status,
			&item.EffectiveFrom,
			&item.EffectiveTo,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) GetAreaByID(ctx context.Context, id string) (domain.Area, error) {
	var item domain.Area
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, parent_id, code, name, area_type_code, admin_unit_code, description, status,
			effective_from::text, effective_to::text, created_at, updated_at
		FROM plt_areas
		WHERE id = $1
		LIMIT 1`, id).
		Scan(
			&item.ID,
			&item.TenantID,
			&item.ParentID,
			&item.Code,
			&item.Name,
			&item.AreaTypeCode,
			&item.AdminUnitCode,
			&item.Description,
			&item.Status,
			&item.EffectiveFrom,
			&item.EffectiveTo,
			&item.CreatedAt,
			&item.UpdatedAt,
		)
	return item, err
}

func (r *PlatformRepository) CreateArea(ctx context.Context, item domain.Area) (domain.Area, error) {
	if item.ID == "" {
		item.ID = NewID("area")
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_areas (id, tenant_id, parent_id, code, name, area_type_code, admin_unit_code, description, status, effective_from, effective_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11::date)
		RETURNING id, tenant_id, parent_id, code, name, area_type_code, admin_unit_code, description, status,
			effective_from::text, effective_to::text, created_at, updated_at`,
		item.ID, item.TenantID, item.ParentID, item.Code, item.Name, item.AreaTypeCode, item.AdminUnitCode, item.Description, item.Status, item.EffectiveFrom, item.EffectiveTo,
	).Scan(
		&item.ID,
		&item.TenantID,
		&item.ParentID,
		&item.Code,
		&item.Name,
		&item.AreaTypeCode,
		&item.AdminUnitCode,
		&item.Description,
		&item.Status,
		&item.EffectiveFrom,
		&item.EffectiveTo,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *PlatformRepository) UpdateArea(ctx context.Context, item domain.Area) (domain.Area, error) {
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_areas
		SET parent_id = $2, code = $3, name = $4, area_type_code = $5, admin_unit_code = $6, description = $7,
			status = $8, effective_from = $9::date, effective_to = $10::date, updated_at = now()
		WHERE id = $1
		RETURNING id, tenant_id, parent_id, code, name, area_type_code, admin_unit_code, description, status,
			effective_from::text, effective_to::text, created_at, updated_at`,
		item.ID, item.ParentID, item.Code, item.Name, item.AreaTypeCode, item.AdminUnitCode, item.Description, item.Status, item.EffectiveFrom, item.EffectiveTo,
	).Scan(
		&item.ID,
		&item.TenantID,
		&item.ParentID,
		&item.Code,
		&item.Name,
		&item.AreaTypeCode,
		&item.AdminUnitCode,
		&item.Description,
		&item.Status,
		&item.EffectiveFrom,
		&item.EffectiveTo,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *PlatformRepository) DeleteArea(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE plt_areas SET status = 'inactive', updated_at = now() WHERE id = $1`, id)
	return err
}

func (r *PlatformRepository) ListFileTemplates(ctx context.Context, tenantID string) ([]domain.FileTemplate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, description, file_type, file_url, mapping_config::text, is_active, created_at, updated_at
		FROM plt_file_templates
		WHERE ($1 = '' OR tenant_id = $1)
		ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.FileTemplate, 0)
	for rows.Next() {
		var item domain.FileTemplate
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.FileType, &item.FileURL, &item.MappingConfig, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformRepository) GetFileTemplateByID(ctx context.Context, id string) (domain.FileTemplate, error) {
	var item domain.FileTemplate
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, description, file_type, file_url, mapping_config::text, is_active, created_at, updated_at
		FROM plt_file_templates
		WHERE id = $1 LIMIT 1`, id).
		Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.FileType, &item.FileURL, &item.MappingConfig, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) CreateFileTemplate(ctx context.Context, item domain.FileTemplate) (domain.FileTemplate, error) {
	if item.ID == "" {
		item.ID = NewID("tmpl")
	}
	if item.TenantID == "" {
		item.TenantID = "default"
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_file_templates (id, tenant_id, code, name, description, file_type, file_url, mapping_config, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, code, name, description, file_type, file_url, mapping_config::text, is_active, created_at, updated_at`,
		item.ID, item.TenantID, item.Code, item.Name, item.Description, item.FileType, item.FileURL, item.MappingConfig, item.IsActive,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.FileType, &item.FileURL, &item.MappingConfig, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) UpdateFileTemplate(ctx context.Context, item domain.FileTemplate) (domain.FileTemplate, error) {
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_file_templates
		SET code = $2, name = $3, description = $4, file_type = $5, file_url = $6, mapping_config = $7, is_active = $8, updated_at = now()
		WHERE id = $1
		RETURNING id, tenant_id, code, name, description, file_type, file_url, mapping_config::text, is_active, created_at, updated_at`,
		item.ID, item.Code, item.Name, item.Description, item.FileType, item.FileURL, item.MappingConfig, item.IsActive,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.FileType, &item.FileURL, &item.MappingConfig, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) DeleteFileTemplate(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_file_templates WHERE id = $1`, id)
	return err
}
