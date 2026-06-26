-- +goose Up

CREATE TABLE IF NOT EXISTS plt_system_parameters (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    key VARCHAR(160) NOT NULL,
    value TEXT NOT NULL,
    value_type VARCHAR(32) NOT NULL DEFAULT 'string',
    scope_type VARCHAR(32) NOT NULL DEFAULT 'global',
    scope_id VARCHAR(64),
    description TEXT,
    is_secret BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_plt_parameters_scope CHECK (scope_type IN ('global', 'tenant', 'org', 'branch', 'department')),
    CONSTRAINT chk_plt_parameters_global_scope CHECK ((scope_type = 'global' AND scope_id IS NULL) OR scope_type <> 'global')
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_plt_parameters_scope
    ON plt_system_parameters (key, scope_type, COALESCE(scope_id, ''), COALESCE(tenant_id, ''));

CREATE TABLE IF NOT EXISTS plt_lookup_categories (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    code VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    scope_type VARCHAR(32) NOT NULL DEFAULT 'global',
    scope_id VARCHAR(64),
    is_system BOOLEAN NOT NULL DEFAULT false,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_plt_lookup_categories_scope CHECK (scope_type IN ('global', 'tenant', 'org', 'branch', 'department')),
    CONSTRAINT chk_plt_lookup_categories_global_scope CHECK ((scope_type = 'global' AND scope_id IS NULL) OR scope_type <> 'global')
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_plt_lookup_categories_scope
    ON plt_lookup_categories (code, scope_type, COALESCE(scope_id, ''), COALESCE(tenant_id, ''));

CREATE TABLE IF NOT EXISTS plt_lookup_values (
    id VARCHAR(64) PRIMARY KEY,
    category_id VARCHAR(64) NOT NULL REFERENCES plt_lookup_categories(id) ON DELETE CASCADE,
    code VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT true,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(category_id, code)
);

CREATE INDEX IF NOT EXISTS idx_plt_lookup_values_category
    ON plt_lookup_values(category_id, is_active, sort_order, name);

CREATE TABLE IF NOT EXISTS plt_organizations (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    parent_id VARCHAR(64) REFERENCES plt_organizations(id),
    code VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    org_type VARCHAR(32) NOT NULL,
    admin_unit_code VARCHAR(20),
    address TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_plt_organizations_type CHECK (org_type IN ('company', 'legal_entity', 'division', 'department', 'branch', 'team'))
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_plt_organizations_tenant_code
    ON plt_organizations(tenant_id, code);
CREATE INDEX IF NOT EXISTS idx_plt_organizations_parent
    ON plt_organizations(tenant_id, parent_id);

CREATE TABLE IF NOT EXISTS geo_admin_units (
    code VARCHAR(20) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    full_name VARCHAR(255),
    parent_code VARCHAR(20) REFERENCES geo_admin_units(code),
    level INT NOT NULL,
    unit_type VARCHAR(32) NOT NULL,
    country_code VARCHAR(2) NOT NULL DEFAULT 'VN',
    region_code VARCHAR(64),
    effective_from DATE NOT NULL DEFAULT CURRENT_DATE,
    effective_to DATE,
    is_active BOOLEAN NOT NULL DEFAULT true,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_geo_admin_units_level CHECK (level BETWEEN 1 AND 5),
    CONSTRAINT chk_geo_admin_units_effective CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE INDEX IF NOT EXISTS idx_geo_admin_units_parent
    ON geo_admin_units(parent_code, is_active, name);
CREATE INDEX IF NOT EXISTS idx_geo_admin_units_level
    ON geo_admin_units(country_code, level, is_active, name);

CREATE TABLE IF NOT EXISTS geo_admin_unit_aliases (
    id VARCHAR(64) PRIMARY KEY,
    admin_unit_code VARCHAR(20) NOT NULL REFERENCES geo_admin_units(code),
    old_code VARCHAR(20),
    old_name VARCHAR(255),
    source VARCHAR(120),
    effective_from DATE,
    effective_to DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS geo_admin_unit_aliases;
DROP TABLE IF EXISTS geo_admin_units;
DROP TABLE IF EXISTS plt_organizations;
DROP TABLE IF EXISTS plt_lookup_values;
DROP TABLE IF EXISTS plt_lookup_categories;
DROP TABLE IF EXISTS plt_system_parameters;
