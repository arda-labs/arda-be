-- +goose Up

-- DPM interest depth (W3): rate tiers + register/adjust approval + accruals +
-- pay/capitalize operations (+ batch grouping). PROVISIONAL accounts/cards live
-- in the finance migration 20260911041000_dpm_interest_accounts.sql.

CREATE TABLE IF NOT EXISTS dpm_interest_rates (
    id             VARCHAR(64) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    product_code   VARCHAR(64),                 -- NULL = default bucket
    term_months    INTEGER NOT NULL DEFAULT 0,
    method         VARCHAR(16) NOT NULL DEFAULT 'SIMPLE', -- SIMPLE|COMPOUND
    denominator    INTEGER NOT NULL DEFAULT 365,          -- 360|365
    rate           NUMERIC(9,6) NOT NULL,
    effective_from DATE NOT NULL,
    is_active      BOOLEAN NOT NULL DEFAULT true,
    created_by     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_dpm_rate_bucket
    ON dpm_interest_rates (tenant_id, COALESCE(product_code, ''), term_months, effective_from);

CREATE TABLE IF NOT EXISTS dpm_rate_requests (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    request_type     VARCHAR(16) NOT NULL, -- REGISTER|EDIT|ADJUST
    payload          JSONB NOT NULL DEFAULT '{}',
    status           VARCHAR(16) NOT NULL DEFAULT 'DRAFT', -- DRAFT|SUBMITTED|APPLIED|REJECTED
    workflow_case_id UUID,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_dpm_rate_req_tenant ON dpm_rate_requests (tenant_id, status);

CREATE TABLE IF NOT EXISTS dpm_accruals (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    savings_id       VARCHAR(64) NOT NULL REFERENCES dpm_savings(id),
    savings_code     VARCHAR(64) NOT NULL,
    period_from      DATE NOT NULL,
    period_to        DATE NOT NULL,
    days             INTEGER NOT NULL,
    base_minor       BIGINT NOT NULL,
    rate             NUMERIC(9,6) NOT NULL,
    amount_minor     BIGINT NOT NULL,
    status           VARCHAR(16) NOT NULL DEFAULT 'POSTED',
    journal_entry_id UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, savings_id, period_to)
);

CREATE INDEX IF NOT EXISTS idx_dpm_accrual_tenant ON dpm_accruals (tenant_id, savings_code);

CREATE TABLE IF NOT EXISTS dpm_interest_ops (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    savings_id       VARCHAR(64) NOT NULL REFERENCES dpm_savings(id),
    savings_code     VARCHAR(64) NOT NULL,
    op_type          VARCHAR(16) NOT NULL, -- PAY|CAPITALIZE
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    days             INTEGER NOT NULL DEFAULT 0,
    rate             NUMERIC(9,6) NOT NULL DEFAULT 0,
    period_from      DATE,
    period_to        DATE,
    batch_id         UUID,
    status           VARCHAR(16) NOT NULL DEFAULT 'DRAFT', -- DRAFT|SUBMITTED|POSTED|REJECTED
    workflow_case_id UUID,
    journal_entry_id UUID,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_dpm_interest_op_tenant ON dpm_interest_ops (tenant_id, savings_id);
CREATE INDEX IF NOT EXISTS idx_dpm_interest_op_batch ON dpm_interest_ops (tenant_id, batch_id);

-- +goose Down

DROP INDEX IF EXISTS idx_dpm_interest_op_batch;
DROP INDEX IF EXISTS idx_dpm_interest_op_tenant;
DROP TABLE IF EXISTS dpm_interest_ops;
DROP INDEX IF EXISTS idx_dpm_accrual_tenant;
DROP TABLE IF EXISTS dpm_accruals;
DROP INDEX IF EXISTS idx_dpm_rate_req_tenant;
DROP TABLE IF EXISTS dpm_rate_requests;
DROP INDEX IF EXISTS uq_dpm_rate_bucket;
DROP TABLE IF EXISTS dpm_interest_rates;
