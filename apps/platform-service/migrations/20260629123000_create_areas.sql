-- +goose Up
CREATE TABLE IF NOT EXISTS plt_areas (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    parent_id VARCHAR(64) REFERENCES plt_areas(id),
    code VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    area_type_code VARCHAR(120) NOT NULL,
    admin_unit_code VARCHAR(20) REFERENCES geo_admin_units(code),
    description TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    effective_from DATE,
    effective_to DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_plt_areas_status CHECK (status IN ('active', 'inactive')),
    CONSTRAINT chk_plt_areas_effective CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_plt_areas_code
    ON plt_areas(tenant_id, code);

CREATE INDEX IF NOT EXISTS idx_plt_areas_parent
    ON plt_areas(tenant_id, parent_id);

CREATE INDEX IF NOT EXISTS idx_plt_areas_type_status
    ON plt_areas(tenant_id, area_type_code, status, name);

-- +goose Down
DROP TABLE IF EXISTS plt_areas;
