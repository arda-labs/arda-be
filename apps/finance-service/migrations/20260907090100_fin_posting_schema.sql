-- +goose Up

-- P1a.1 posting schema (docs/accounting-posting-contract.md v0.2 +
-- docs/db-schema-conventions.md): amounts are int64 minor units (BIGINT),
-- ids UUIDv7, tenant_id VARCHAR(64) = tenant UUID from iam registry,
-- standard audit columns + optimistic version.

CREATE SEQUENCE IF NOT EXISTS fin_journal_entry_no_seq;

CREATE TABLE IF NOT EXISTS fin_periods (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    period_code   VARCHAR(7) NOT NULL,              -- "2026-09"
    start_date    DATE NOT NULL,
    end_date      DATE NOT NULL,
    status        TEXT NOT NULL DEFAULT 'OPEN',     -- OPEN | CLOSED
    closed_at     TIMESTAMPTZ,
    closed_by     TEXT,
    created_by    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       INTEGER NOT NULL DEFAULT 1,
    CHECK (end_date >= start_date),
    UNIQUE (tenant_id, period_code),
    UNIQUE (tenant_id, start_date)
);

CREATE TABLE IF NOT EXISTS fin_journal_entries (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           VARCHAR(64) NOT NULL,
    entry_no            BIGINT NOT NULL DEFAULT nextval('fin_journal_entry_no_seq'),
    accounting_date     DATE NOT NULL,
    currency_code       VARCHAR(3) NOT NULL,
    status              TEXT NOT NULL DEFAULT 'POSTED', -- POSTED | REVERSED
    description         TEXT,
    -- business reference (chứng từ gốc, contract §3)
    business_domain     TEXT NOT NULL,                  -- lnm | dpm | ibm | cfc | vcm | cob | fin
    business_doc_type   TEXT NOT NULL,                  -- DISBURSEMENT | COLLECTION | ACCRUAL | ...
    business_doc_id     UUID,
    business_doc_code   TEXT,
    case_id             UUID,
    -- idempotency (contract §5)
    idempotency_key     TEXT,
    -- reversal: original entry points at its reversal (original stays immutable)
    reversed_by_entry_id UUID REFERENCES fin_journal_entries(id),
    created_by          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          TEXT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    version             INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, entry_no)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_entry_idem
    ON fin_journal_entries (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_fin_entry_date
    ON fin_journal_entries (tenant_id, accounting_date DESC);
CREATE INDEX IF NOT EXISTS idx_fin_entry_doc
    ON fin_journal_entries (tenant_id, business_domain, business_doc_type, business_doc_id);

CREATE TABLE IF NOT EXISTS fin_journal_lines (
    id                 UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          VARCHAR(64) NOT NULL,
    entry_id           UUID NOT NULL REFERENCES fin_journal_entries(id) ON DELETE CASCADE,
    line_no            INTEGER NOT NULL,
    direction          TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    -- resolved account (COA v2): code + version snapshot at posting time
    coa_version        TEXT NOT NULL,
    account_code       TEXT NOT NULL,
    account_name       TEXT,
    amount_minor       BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code      VARCHAR(3) NOT NULL,
    counterparty_code  TEXT,
    counterparty_name  TEXT,
    description        TEXT,
    analytics          JSONB NOT NULL DEFAULT '{}',   -- resolved dimensions (registry-validated)
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entry_id, line_no)
);

CREATE INDEX IF NOT EXISTS idx_fin_line_account
    ON fin_journal_lines (tenant_id, account_code, coa_version);
CREATE INDEX IF NOT EXISTS idx_fin_line_entry
    ON fin_journal_lines (entry_id, direction);

CREATE TABLE IF NOT EXISTS fin_opening_balances (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    accounting_date  DATE NOT NULL,
    coa_version      TEXT NOT NULL,
    account_code     TEXT NOT NULL,
    currency_code    VARCHAR(3) NOT NULL,
    direction        TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    description      TEXT,
    source_key       TEXT,                            -- EPAS migration mapping (Q6)
    created_by       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, accounting_date, coa_version, account_code, currency_code)
);

CREATE TABLE IF NOT EXISTS fin_dimension_keys (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id       VARCHAR(64),                     -- NULL = global registry row
    key             TEXT NOT NULL,
    data_type       TEXT NOT NULL DEFAULT 'STRING',  -- STRING | NUMBER | CODE
    required_for    TEXT[] NOT NULL DEFAULT '{}',    -- business doc types requiring it
    ref_catalog     TEXT,                            -- optional mdm catalog code
    description     TEXT,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_by      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    version         INTEGER NOT NULL DEFAULT 1,
    UNIQUE (COALESCE(tenant_id, ''), key)
);

CREATE TABLE IF NOT EXISTS fin_accounting_rules (
    id                   UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id            VARCHAR(64) NOT NULL,
    document_type        TEXT NOT NULL,              -- LNM_DISBURSEMENT | LNM_COLLECTION | ...
    line_no              INTEGER NOT NULL,
    direction            TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    resolution_type      TEXT NOT NULL DEFAULT 'CLASS_MAP', -- CLASS_MAP | FIXED_CODE
    account_ref          TEXT,                       -- fixed code when FIXED_CODE
    acc_classification   TEXT,                       -- analytics when CLASS_MAP
    debt_group_code      TEXT,
    currency_code        TEXT,                       -- NULL = any currency
    required_dimensions  TEXT[] NOT NULL DEFAULT '{}',
    description_template TEXT,
    is_active            BOOLEAN NOT NULL DEFAULT true,
    created_by           TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by           TEXT,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    version              INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, document_type, line_no)
);

-- Transactional outbox (contract §7) — published by the P1a outbox relay.
CREATE TABLE IF NOT EXISTS fin_outbox (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    event_type       TEXT NOT NULL,                  -- finance.journal.posted.v1
    aggregate_id     UUID,
    payload          JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at     TIMESTAMPTZ,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT
);

CREATE INDEX IF NOT EXISTS idx_fin_outbox_pending
    ON fin_outbox (created_at) WHERE published_at IS NULL;

-- +goose Down
-- No down: rebuild mode — superseding schema, not replaceable.
SELECT 1;
