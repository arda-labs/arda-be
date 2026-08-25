package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

type PlatformRepository struct {
	db *sql.DB
}

func NewPlatformRepository(db *sql.DB) *PlatformRepository {
	return &PlatformRepository{db: db}
}

func requireTenantID(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("tenant scope is required")
	}
	return nil
}

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure platform id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func (r *PlatformRepository) ListParameters(ctx context.Context, tenantID, scopeType, scopeID string) ([]domain.Parameter, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
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
	if err := requireTenantID(tenantID); err != nil {
		return domain.Parameter{}, err
	}
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

// GetGlobalParameter resolves a global setting without requiring a tenant.
// Global parameters are deliberately stored with both tenant_id and scope_id
// NULL; this is the public/control-plane scope and must not go through the
// tenant-scoped GetParameter path.
func (r *PlatformRepository) GetGlobalParameter(ctx context.Context, key string) (domain.Parameter, error) {
	var item domain.Parameter
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, key, value, value_type, scope_type, scope_id, description, is_secret, created_at, updated_at
		FROM plt_system_parameters
		WHERE key = $1
		  AND scope_type = $2
		  AND scope_id IS NULL
		  AND tenant_id IS NULL
		LIMIT 1`, key, domain.ScopeGlobal).
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
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
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

func (r *PlatformRepository) ListLookupValues(ctx context.Context, tenantID, categoryCode string) ([]domain.LookupValue, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.id, v.category_id, v.code, v.name, v.sort_order, v.is_active, v.metadata::text, v.created_at, v.updated_at
		FROM plt_lookup_values v
		JOIN plt_lookup_categories c ON c.id = v.category_id
		WHERE c.tenant_id = $1 AND c.code = $2
		ORDER BY v.sort_order, v.name`, tenantID, categoryCode)
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

func (r *PlatformRepository) CreateLookupValue(ctx context.Context, tenantID, categoryCode string, item domain.LookupValue) (domain.LookupValue, error) {
	if err := requireTenantID(tenantID); err != nil {
		return domain.LookupValue{}, err
	}
	if item.ID == "" {
		item.ID = NewID("lookup_val")
	}
	if !item.IsActive {
		item.IsActive = true
	}
	err := r.db.QueryRowContext(ctx, `SELECT id FROM plt_lookup_categories WHERE tenant_id = $1 AND code = $2 LIMIT 1`, tenantID, categoryCode).Scan(&item.CategoryID)
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

func (r *PlatformRepository) UpsertLookupValue(ctx context.Context, tenantID, categoryCode string, item domain.LookupValue) (domain.LookupValue, error) {
	if err := requireTenantID(tenantID); err != nil {
		return domain.LookupValue{}, err
	}
	if item.ID == "" {
		item.ID = NewID("lookup_val")
	}
	if !item.IsActive {
		item.IsActive = true
	}
	err := r.db.QueryRowContext(ctx, `SELECT id FROM plt_lookup_categories WHERE tenant_id = $1 AND code = $2 LIMIT 1`, tenantID, categoryCode).Scan(&item.CategoryID)
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

func (r *PlatformRepository) ListOrganizations(ctx context.Context, params ListOrganizationsParams) ([]domain.Organization, int, error) {
	if err := requireTenantID(params.TenantID); err != nil {
		return nil, 0, err
	}
	where := []string{"($1 = '' OR o.tenant_id = $1)"}
	args := []any{params.TenantID}
	argN := 2

	if params.Query != "" {
		where = append(where, fmt.Sprintf("(o.name ILIKE $%d OR o.code ILIKE $%d)", argN, argN))
		args = append(args, "%"+params.Query+"%")
		argN++
	}
	if params.IsActive != nil {
		where = append(where, fmt.Sprintf("o.is_active = $%d", argN))
		args = append(args, *params.IsActive)
		argN++
	}
	whereClause := strings.Join(where, " AND ")

	sortColumn := pickOrganizationSort(params.Sort)
	order := "ASC"
	if strings.EqualFold(params.Order, "desc") {
		order = "DESC"
	}
	orderBy := fmt.Sprintf("%s %s", sortColumn, order)
	if params.Unpaged && strings.TrimSpace(params.Sort) == "" {
		orderBy = "o.parent_id NULLS FIRST, o.code ASC"
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM plt_organizations o WHERE %s`, whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listQuery := fmt.Sprintf(`
		SELECT o.id, o.tenant_id, o.parent_id, p.name, o.code, o.name, o.admin_unit_code, o.address, o.is_active, o.created_at, o.updated_at
		FROM plt_organizations o
		LEFT JOIN plt_organizations p ON p.id = o.parent_id
		WHERE %s
		ORDER BY %s`, whereClause, orderBy)

	listArgs := append([]any{}, args...)
	if !params.Unpaged {
		listQuery += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argN, argN+1)
		listArgs = append(listArgs, params.PerPage, params.Offset)
	}

	rows, err := r.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]domain.Organization, 0)
	for rows.Next() {
		var item domain.Organization
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.ParentID, &item.ParentName,
			&item.Code, &item.Name, &item.AdminUnitCode, &item.Address, &item.IsActive,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

type ListOrganizationsParams struct {
	TenantID string
	Page     int
	PerPage  int
	Offset   int
	Query    string
	IsActive *bool
	Sort     string
	Order    string
	Unpaged  bool
}

func pickOrganizationSort(field string) string {
	switch strings.TrimSpace(field) {
	case "name":
		return "o.name"
	case "is_active":
		return "o.is_active"
	case "created_at":
		return "o.created_at"
	default:
		return "o.code"
	}
}

func (r *PlatformRepository) CreateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.Organization{}, err
	}
	if item.ID == "" {
		item.ID = NewID("org")
	}
	if item.TenantID == "" {
		return domain.Organization{}, fmt.Errorf("tenant id is required")
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

func (r *PlatformRepository) GetOrganizationByID(ctx context.Context, tenantID, id string) (domain.Organization, error) {
	if err := requireTenantID(tenantID); err != nil {
		return domain.Organization{}, err
	}
	var item domain.Organization
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, parent_id, code, name, admin_unit_code, address, is_active, created_at, updated_at
		FROM plt_organizations
		WHERE tenant_id = $1 AND id = $2 LIMIT 1`, tenantID, id).
		Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) UpdateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.Organization{}, err
	}
	if item.TenantID == "" {
		return domain.Organization{}, fmt.Errorf("tenant id is required")
	}
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_organizations
		SET parent_id = $3, code = $4, name = $5, admin_unit_code = $6, address = $7, is_active = $8, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, parent_id, code, name, admin_unit_code, address, is_active, created_at, updated_at`,
		item.TenantID, item.ID, item.ParentID, item.Code, item.Name, item.AdminUnitCode, item.Address, item.IsActive,
	).Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Name, &item.AdminUnitCode, &item.Address, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) DeleteOrganization(ctx context.Context, tenantID, id string) error {
	if err := requireTenantID(tenantID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `UPDATE plt_organizations SET is_active = false, updated_at = now() WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

func (r *PlatformRepository) DeleteParameter(ctx context.Context, tenantID, id string) error {
	if err := requireTenantID(tenantID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_system_parameters WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

func (r *PlatformRepository) DeleteLookupCategory(ctx context.Context, tenantID, id string) error {
	if err := requireTenantID(tenantID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_lookup_categories WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

func (r *PlatformRepository) DeleteLookupValue(ctx context.Context, tenantID, id string) error {
	if err := requireTenantID(tenantID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM plt_lookup_values v
		USING plt_lookup_categories c
		WHERE v.id = $2 AND v.category_id = c.id AND c.tenant_id = $1`, tenantID, id)
	return err
}

func (r *PlatformRepository) ListCreditInstitutions(ctx context.Context, tenantID, status, query string) ([]domain.CreditInstitution, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
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

func (r *PlatformRepository) GetCreditInstitutionByID(ctx context.Context, tenantID, id string) (domain.CreditInstitution, error) {
	if err := requireTenantID(tenantID); err != nil {
		return domain.CreditInstitution{}, err
	}
	var item domain.CreditInstitution
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, address, status, effective_from::text, short_name, phone, email,
			license_no, license_date::text, tax_code, website, note, created_at, updated_at
		FROM plt_credit_institutions
		WHERE tenant_id = $1 AND id = $2
		LIMIT 1`, tenantID, id).
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
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.CreditInstitution{}, err
	}
	if item.ID == "" {
		item.ID = NewID("ci")
	}
	if item.TenantID == "" {
		return domain.CreditInstitution{}, fmt.Errorf("tenant id is required")
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
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.CreditInstitution{}, err
	}
	if item.TenantID == "" {
		return domain.CreditInstitution{}, fmt.Errorf("tenant id is required")
	}
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_credit_institutions
		SET code = $3, name = $4, address = $5, status = $6, effective_from = $7::date, short_name = $8, phone = $9, email = $10,
			license_no = $11, license_date = $12::date, tax_code = $13, website = $14, note = $15, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, code, name, address, status, effective_from::text, short_name, phone, email,
			license_no, license_date::text, tax_code, website, note, created_at, updated_at`,
		item.TenantID, item.ID, item.Code, item.Name, item.Address, item.Status, item.EffectiveFrom, item.ShortName, item.Phone, item.Email,
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

func (r *PlatformRepository) DeleteCreditInstitution(ctx context.Context, tenantID, id string) error {
	if err := requireTenantID(tenantID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_credit_institutions WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

func (r *PlatformRepository) ListAreas(ctx context.Context, tenantID, status, areaTypeCode, parentID, query string) ([]domain.Area, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
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

func (r *PlatformRepository) GetAreaByID(ctx context.Context, tenantID, id string) (domain.Area, error) {
	if err := requireTenantID(tenantID); err != nil {
		return domain.Area{}, err
	}
	var item domain.Area
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, parent_id, code, name, area_type_code, admin_unit_code, description, status,
			effective_from::text, effective_to::text, created_at, updated_at
		FROM plt_areas
		WHERE tenant_id = $1 AND id = $2
		LIMIT 1`, tenantID, id).
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
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.Area{}, err
	}
	if item.ID == "" {
		item.ID = NewID("area")
	}
	if item.TenantID == "" {
		return domain.Area{}, fmt.Errorf("tenant id is required")
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
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.Area{}, err
	}
	if item.TenantID == "" {
		return domain.Area{}, fmt.Errorf("tenant id is required")
	}
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_areas
		SET parent_id = $3, code = $4, name = $5, area_type_code = $6, admin_unit_code = $7, description = $8,
			status = $9, effective_from = $10::date, effective_to = $11::date, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, parent_id, code, name, area_type_code, admin_unit_code, description, status,
			effective_from::text, effective_to::text, created_at, updated_at`,
		item.TenantID, item.ID, item.ParentID, item.Code, item.Name, item.AreaTypeCode, item.AdminUnitCode, item.Description, item.Status, item.EffectiveFrom, item.EffectiveTo,
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

func (r *PlatformRepository) DeleteArea(ctx context.Context, tenantID, id string) error {
	if err := requireTenantID(tenantID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `UPDATE plt_areas SET status = 'inactive', updated_at = now() WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

func (r *PlatformRepository) ListFileTemplates(ctx context.Context, tenantID string) ([]domain.FileTemplate, error) {
	if err := requireTenantID(tenantID); err != nil {
		return nil, err
	}
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

func (r *PlatformRepository) GetFileTemplateByID(ctx context.Context, tenantID, id string) (domain.FileTemplate, error) {
	if err := requireTenantID(tenantID); err != nil {
		return domain.FileTemplate{}, err
	}
	var item domain.FileTemplate
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, description, file_type, file_url, mapping_config::text, is_active, created_at, updated_at
		FROM plt_file_templates
		WHERE tenant_id = $1 AND id = $2 LIMIT 1`, tenantID, id).
		Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.FileType, &item.FileURL, &item.MappingConfig, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) CreateFileTemplate(ctx context.Context, item domain.FileTemplate) (domain.FileTemplate, error) {
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.FileTemplate{}, err
	}
	if item.ID == "" {
		item.ID = NewID("tmpl")
	}
	if item.TenantID == "" {
		return domain.FileTemplate{}, fmt.Errorf("tenant id is required")
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
	if err := requireTenantID(item.TenantID); err != nil {
		return domain.FileTemplate{}, err
	}
	if item.TenantID == "" {
		return domain.FileTemplate{}, fmt.Errorf("tenant id is required")
	}
	err := r.db.QueryRowContext(ctx, `
		UPDATE plt_file_templates
		SET code = $3, name = $4, description = $5, file_type = $6, file_url = $7, mapping_config = $8, is_active = $9, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, code, name, description, file_type, file_url, mapping_config::text, is_active, created_at, updated_at`,
		item.TenantID, item.ID, item.Code, item.Name, item.Description, item.FileType, item.FileURL, item.MappingConfig, item.IsActive,
	).Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.FileType, &item.FileURL, &item.MappingConfig, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PlatformRepository) DeleteFileTemplate(ctx context.Context, tenantID, id string) error {
	if err := requireTenantID(tenantID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM plt_file_templates WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}
