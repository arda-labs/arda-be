-- +goose Up

-- P2.4b: VCM treasury cash transactions (Q3: VCM merged into finance).
-- Cash in/out per denomination-aggregated cash position; posts via
-- PostingService (VCM_CASH_IN / VCM_CASH_OUT document types).

CREATE TABLE IF NOT EXISTS fin_cash_transactions (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL,
    txn_date       DATE NOT NULL,
    direction      TEXT NOT NULL CHECK (direction IN ('IN','OUT')),
    amount_minor   BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code  VARCHAR(3) NOT NULL DEFAULT 'VND',
    org_code       VARCHAR(64),
    description    TEXT,
    journal_entry_id UUID,
    created_by     TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_fin_cash_tenant ON fin_cash_transactions (tenant_id, txn_date);

-- +goose Down
SELECT 1;
