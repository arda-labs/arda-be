-- +goose Up

-- CFM lifecycle (W1): contract formation/amendments are maker-checker cases;
-- movements (receipt/disbursement/payment/settlement) stage through the
-- workflow before posting. Products catalog added for EPAS "Sản phẩm (vốn)".

CREATE TABLE IF NOT EXISTS cfc_contract_amendments (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    contract_id      UUID NOT NULL REFERENCES cfc_contracts(id),
    status           VARCHAR(16) NOT NULL DEFAULT 'DRAFT', -- DRAFT|SUBMITTED|APPLIED|REJECTED
    payload          JSONB NOT NULL DEFAULT '{}',
    reason           TEXT NOT NULL DEFAULT '',
    workflow_case_id UUID,
    submitted_by     TEXT,
    submitted_at     TIMESTAMPTZ,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_cfc_amendment_tenant ON cfc_contract_amendments (tenant_id, contract_id);

CREATE TABLE IF NOT EXISTS cfc_products (
    id             VARCHAR(64) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    code           VARCHAR(64) NOT NULL,
    name           VARCHAR(255) NOT NULL,
    fund_type_code VARCHAR(64) NOT NULL,
    term_months    INTEGER NOT NULL DEFAULT 0,
    interest_rate  NUMERIC(9,6) NOT NULL DEFAULT 0,
    currency_code  VARCHAR(3) NOT NULL DEFAULT 'VND',
    is_active      BOOLEAN NOT NULL DEFAULT true,
    created_by     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

ALTER TABLE cfc_contracts ADD COLUMN IF NOT EXISTS product_code  VARCHAR(64);
ALTER TABLE cfc_contracts ADD COLUMN IF NOT EXISTS maturity_date DATE;

ALTER TABLE cfc_movements ADD COLUMN IF NOT EXISTS note            TEXT NOT NULL DEFAULT '';
ALTER TABLE cfc_movements ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(128);

CREATE UNIQUE INDEX IF NOT EXISTS uq_cfc_movement_idem
    ON cfc_movements (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS uq_cfc_movement_idem;
ALTER TABLE cfc_movements DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE cfc_movements DROP COLUMN IF EXISTS note;
ALTER TABLE cfc_contracts DROP COLUMN IF EXISTS maturity_date;
ALTER TABLE cfc_contracts DROP COLUMN IF EXISTS product_code;
DROP TABLE IF EXISTS cfc_products;
DROP TABLE IF EXISTS cfc_contract_amendments;
