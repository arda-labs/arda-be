-- +goose Up

-- IBM lifecycle (W2): products catalog + deposit contract staging + movements.
-- PLACE stages an ibm_deposits row (PENDING_APPROVAL → ACTIVE on checker
-- APPROVE); TOP_UP/INTEREST/EXPECTED/WITHDRAW stage ibm_movements rows.

CREATE TABLE IF NOT EXISTS ibm_products (
    id             VARCHAR(64) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    code           VARCHAR(64) NOT NULL,
    name           VARCHAR(255) NOT NULL,
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

ALTER TABLE ibm_deposits ADD COLUMN IF NOT EXISTS product_code       VARCHAR(64);
ALTER TABLE ibm_deposits ADD COLUMN IF NOT EXISTS counterparty_name  VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE ibm_deposits ADD COLUMN IF NOT EXISTS workflow_case_id   UUID;
ALTER TABLE ibm_deposits ADD COLUMN IF NOT EXISTS journal_entry_id   UUID;
ALTER TABLE ibm_deposits ADD COLUMN IF NOT EXISTS last_interest_date DATE;

CREATE TABLE IF NOT EXISTS ibm_movements (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    deposit_id       VARCHAR(64) NOT NULL REFERENCES ibm_deposits(id),
    kind             VARCHAR(16) NOT NULL, -- TOP_UP|INTEREST|EXPECTED|WITHDRAW
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code    VARCHAR(3) NOT NULL DEFAULT 'VND',
    movement_date    DATE NOT NULL,
    period_from      DATE,
    period_to        DATE,
    note             TEXT NOT NULL DEFAULT '',
    idempotency_key  VARCHAR(128),
    status           VARCHAR(16) NOT NULL DEFAULT 'DRAFT', -- DRAFT|SUBMITTED|POSTED|REJECTED
    workflow_case_id UUID,
    journal_entry_id UUID,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_ibm_movement_tenant ON ibm_movements (tenant_id, deposit_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_ibm_movement_idem
    ON ibm_movements (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS uq_ibm_movement_idem;
DROP TABLE IF EXISTS ibm_movements;
ALTER TABLE ibm_deposits DROP COLUMN IF EXISTS last_interest_date;
ALTER TABLE ibm_deposits DROP COLUMN IF EXISTS journal_entry_id;
ALTER TABLE ibm_deposits DROP COLUMN IF EXISTS workflow_case_id;
ALTER TABLE ibm_deposits DROP COLUMN IF EXISTS counterparty_name;
ALTER TABLE ibm_deposits DROP COLUMN IF EXISTS product_code;
DROP TABLE IF EXISTS ibm_products;
