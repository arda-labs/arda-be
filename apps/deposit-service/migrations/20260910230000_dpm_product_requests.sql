-- +goose Up

-- DPM.102/103 product register/edit requests: the maker submits a staged
-- product payload, the checker approval applies it to dpm_products. The
-- request row is the audit trail (who requested what, which case).

CREATE TABLE IF NOT EXISTS dpm_product_requests (
    id                 VARCHAR(64) PRIMARY KEY,
    tenant_id          VARCHAR(64) NOT NULL,
    request_type       VARCHAR(16) NOT NULL CHECK (request_type IN ('REGISTER', 'EDIT')),
    product_code       VARCHAR(64) NOT NULL,
    name               TEXT NOT NULL,
    term_months        INTEGER NOT NULL,
    interest_rate      NUMERIC(9,6) NOT NULL,
    currency_code      VARCHAR(3) NOT NULL DEFAULT 'VND',
    status             VARCHAR(16) NOT NULL DEFAULT 'SUBMITTED',
    workflow_case_id   VARCHAR(64),
    workflow_case_code VARCHAR(64),
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    version            INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_dpm_product_requests_tenant
    ON dpm_product_requests (tenant_id, status, created_at DESC);

-- +goose Down
SELECT 1;
