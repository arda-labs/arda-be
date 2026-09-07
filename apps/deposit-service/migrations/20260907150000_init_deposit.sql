-- +goose Up

-- P2.1: deposit-service schema (DPM + IBM, Q3). Tables per
-- docs/db-schema-conventions.md; amounts int64 minor.
-- IDs are app-generated prefixed strings (NewDepositID), so PKs/FKs are
-- VARCHAR(64) per loan-service convention, not UUID. journal_entry_id stays
-- UUID because finance-service journal entries are UUID PKs.

CREATE TABLE IF NOT EXISTS dpm_products (
    id               VARCHAR(64) PRIMARY KEY,
    tenant_id        VARCHAR(64) NOT NULL,
    code             VARCHAR(64) NOT NULL,
    name             VARCHAR(255) NOT NULL,
    term_months      INTEGER NOT NULL,
    interest_rate    NUMERIC(9,6) NOT NULL,
    currency_code    VARCHAR(3) NOT NULL DEFAULT 'VND',
    is_active        BOOLEAN NOT NULL DEFAULT true,
    created_by       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS dpm_savings (
    id                VARCHAR(64) PRIMARY KEY,
    tenant_id         VARCHAR(64) NOT NULL,
    savings_code      VARCHAR(64) NOT NULL,
    customer_code     VARCHAR(64) NOT NULL,
    product_code      VARCHAR(64) NOT NULL REFERENCES dpm_products(id),
    open_date         DATE NOT NULL,
    maturity_date     DATE NOT NULL,
    principal_minor   BIGINT NOT NULL CHECK (principal_minor > 0),
    accrued_minor     BIGINT NOT NULL DEFAULT 0,
    currency_code     VARCHAR(3) NOT NULL DEFAULT 'VND',
    org_code          VARCHAR(64),
    status            VARCHAR(16) NOT NULL DEFAULT 'ACTIVE', -- ACTIVE|CLOSED
    workflow_case_id  VARCHAR(64),
    journal_entry_id  UUID,
    created_by        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, savings_code)
);

CREATE INDEX IF NOT EXISTS idx_dpm_savings_tenant ON dpm_savings (tenant_id, status);

CREATE TABLE IF NOT EXISTS dpm_transactions (
    id             VARCHAR(64) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    savings_id     VARCHAR(64) NOT NULL REFERENCES dpm_savings(id),
    txn_type       VARCHAR(32) NOT NULL,   -- OPEN|TOP_UP|WITHDRAW|SETTLE|INTEREST
    amount_minor   BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code  VARCHAR(3) NOT NULL DEFAULT 'VND',
    txn_date       DATE NOT NULL,
    status         VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64),
    journal_entry_id UUID,
    created_by     TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_dpm_txn_tenant ON dpm_transactions (tenant_id, savings_id);

-- IBM: interbank deposits (simpler: contract + transactions)
CREATE TABLE IF NOT EXISTS ibm_deposits (
    id                VARCHAR(64) PRIMARY KEY,
    tenant_id         VARCHAR(64) NOT NULL,
    deposit_code      VARCHAR(64) NOT NULL,
    counterparty_code VARCHAR(64) NOT NULL,   -- tổ chức tín dụng đối tác
    deposit_date      DATE NOT NULL,
    maturity_date     DATE NOT NULL,
    principal_minor   BIGINT NOT NULL CHECK (principal_minor > 0),
    interest_rate     NUMERIC(9,6) NOT NULL,
    accrued_minor     BIGINT NOT NULL DEFAULT 0,
    currency_code     VARCHAR(3) NOT NULL DEFAULT 'VND',
    org_code          VARCHAR(64),
    status            VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_by        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, deposit_code)
);

CREATE INDEX IF NOT EXISTS idx_ibm_deposit_tenant ON ibm_deposits (tenant_id, status);

-- +goose Down
SELECT 1;
