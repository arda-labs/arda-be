-- +goose Up

-- Counterparty master (W4c-E, EPAS fac-inf-acc / fac-inf-acc-bank): partner
-- accounts used by posting resolution + the counterparty catalog screen.

CREATE TABLE IF NOT EXISTS fin_counterparties (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  VARCHAR(64) NOT NULL,
    code       VARCHAR(64) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    party_type VARCHAR(32) NOT NULL DEFAULT 'OTHER', -- INTERNAL|BANK|OTHER
    org_code   VARCHAR(64),
    note       TEXT NOT NULL DEFAULT '',
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE INDEX IF NOT EXISTS idx_fin_counterparty_tenant ON fin_counterparties (tenant_id, party_type);

CREATE TABLE IF NOT EXISTS fin_counterparty_accounts (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    counterparty_id  UUID NOT NULL REFERENCES fin_counterparties(id),
    account_no       VARCHAR(64) NOT NULL,
    bank_code        VARCHAR(64) NOT NULL DEFAULT '',
    coa_account_code VARCHAR(32) NOT NULL DEFAULT '',
    currency_code    VARCHAR(3) NOT NULL DEFAULT 'VND',
    is_default       BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, counterparty_id, account_no)
);

CREATE INDEX IF NOT EXISTS idx_fin_cp_account_cp ON fin_counterparty_accounts (tenant_id, counterparty_id);

-- +goose Down

DROP TABLE IF EXISTS fin_counterparty_accounts;
DROP TABLE IF EXISTS fin_counterparties;
