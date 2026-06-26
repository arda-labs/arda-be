-- +goose Up

CREATE TABLE IF NOT EXISTS plt_lookup_value_translations (
    value_id VARCHAR(64) NOT NULL REFERENCES plt_lookup_values(id) ON DELETE CASCADE,
    locale VARCHAR(16) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (value_id, locale)
);

CREATE TABLE IF NOT EXISTS plt_organization_translations (
    organization_id VARCHAR(64) NOT NULL REFERENCES plt_organizations(id) ON DELETE CASCADE,
    locale VARCHAR(16) NOT NULL,
    name VARCHAR(255) NOT NULL,
    address TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, locale)
);

CREATE TABLE IF NOT EXISTS geo_admin_unit_translations (
    admin_unit_code VARCHAR(20) NOT NULL REFERENCES geo_admin_units(code) ON DELETE CASCADE,
    locale VARCHAR(16) NOT NULL,
    name VARCHAR(255) NOT NULL,
    full_name VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (admin_unit_code, locale)
);

CREATE INDEX IF NOT EXISTS idx_plt_lookup_value_translations_locale
    ON plt_lookup_value_translations(locale, name);

CREATE INDEX IF NOT EXISTS idx_plt_organization_translations_locale
    ON plt_organization_translations(locale, name);

CREATE INDEX IF NOT EXISTS idx_geo_admin_unit_translations_locale
    ON geo_admin_unit_translations(locale, name);

-- +goose Down
DROP TABLE IF EXISTS geo_admin_unit_translations;
DROP TABLE IF EXISTS plt_organization_translations;
DROP TABLE IF EXISTS plt_lookup_value_translations;

