-- +goose Up
CREATE TABLE IF NOT EXISTS plt_credit_institutions (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    address TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    effective_from DATE,
    short_name VARCHAR(120),
    phone VARCHAR(32),
    email VARCHAR(255),
    license_no VARCHAR(120),
    license_date DATE,
    tax_code VARCHAR(64),
    website VARCHAR(255),
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_plt_credit_institutions_status CHECK (status IN ('active', 'inactive'))
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_plt_credit_institutions_code
    ON plt_credit_institutions(tenant_id, code);

CREATE INDEX IF NOT EXISTS idx_plt_credit_institutions_status
    ON plt_credit_institutions(tenant_id, status, name);

-- +goose Down
DROP TABLE IF EXISTS plt_credit_institutions;
