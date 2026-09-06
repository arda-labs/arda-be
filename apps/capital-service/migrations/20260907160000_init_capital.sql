-- +goose Up

-- P2.2: capital-service schema (CFM, Q3). Fund contracts + movements.

CREATE TABLE IF NOT EXISTS cfc_fund_types (
    id         VARCHAR(64) PRIMARY KEY,
    tenant_id  VARCHAR(64) NOT NULL,
    code       VARCHAR(64) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS cfc_contracts (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         VARCHAR(64) NOT NULL,
    contract_code     VARCHAR(64) NOT NULL,
    fund_type_code    VARCHAR(64) NOT NULL REFERENCES cfc_fund_types(id),
    counterparty_code VARCHAR(64) NOT NULL,
    contract_date     DATE NOT NULL,
    amount_minor      BIGINT NOT NULL CHECK (amount_minor > 0),
    interest_rate     NUMERIC(9,6) NOT NULL,
    currency_code     VARCHAR(3) NOT NULL DEFAULT 'VND',
    status            VARCHAR(16) NOT NULL DEFAULT 'ACTIVE', -- ACTIVE|CLOSED
    org_code          VARCHAR(64),
    workflow_case_id  UUID,
    journal_entry_id  UUID,
    created_by        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, contract_code)
);

CREATE INDEX IF NOT EXISTS idx_cfc_contract_tenant ON cfc_contracts (tenant_id, status);

CREATE TABLE IF NOT EXISTS cfc_movements (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL,
    contract_id    UUID NOT NULL REFERENCES cfc_contracts(id),
    movement_type  VARCHAR(32) NOT NULL,   -- RECEIPT|DISBURSEMENT|PAYMENT
    amount_minor   BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code  VARCHAR(3) NOT NULL DEFAULT 'VND',
    movement_date  DATE NOT NULL,
    status         VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id UUID,
    journal_entry_id UUID,
    created_by     TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_cfc_movement_tenant ON cfc_movements (tenant_id, contract_id);

INSERT INTO cfc_fund_types (id, tenant_id, code, name, created_by)
VALUES ('cfc-type-tw', '00000000-0000-0000-0000-000000000010', 'TW', 'Vốn Trung ương', 'seed'),
       ('cfc-type-tinh', '00000000-0000-0000-0000-000000000010', 'TINH', 'Vốn tỉnh', 'seed')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
