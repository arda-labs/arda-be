-- +goose Up
CREATE TABLE IF NOT EXISTS plt_file_templates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    file_type VARCHAR(32) NOT NULL,
    file_url VARCHAR(512) NOT NULL,
    mapping_config JSONB,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_plt_file_templates_code ON plt_file_templates(tenant_id, code);

-- +goose Down
DROP TABLE IF EXISTS plt_file_templates;
