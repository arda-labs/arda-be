-- +goose Up

-- P1b.4b: monthly interest accrual runs (batch EOD, no userTask).
-- One run per tenant+period: computes interest per ACTIVE agreement from
-- last accrual date to run date, posts LNM_ACCRUAL (2 lines) via the
-- finance PostingService, then records the receipt for collection.

CREATE TABLE IF NOT EXISTS lnm_accruals (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    agreement_code   VARCHAR(64) NOT NULL,
    from_date        DATE NOT NULL,
    to_date          DATE NOT NULL,
    interest_minor   BIGINT NOT NULL CHECK (interest_minor >= 0),
    currency_code    VARCHAR(3) NOT NULL DEFAULT 'VND',
    journal_entry_id UUID,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, agreement_code, to_date)
);

CREATE INDEX IF NOT EXISTS idx_lnm_accrual_tenant ON lnm_accruals (tenant_id, to_date);

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
