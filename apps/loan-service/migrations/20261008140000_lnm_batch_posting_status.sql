-- +goose Up

-- Existing rows represent completed postings. New rows are staged PENDING
-- before the finance call and transition to POSTED only after finance succeeds.
ALTER TABLE lnm_accruals
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'POSTED';

ALTER TABLE lnm_provisions
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'POSTED';

ALTER TABLE lnm_accruals
    ADD CONSTRAINT lnm_accruals_status_check CHECK (status IN ('PENDING', 'POSTED'));

ALTER TABLE lnm_provisions
    ADD CONSTRAINT lnm_provisions_status_check CHECK (status IN ('PENDING', 'POSTED'));

CREATE INDEX IF NOT EXISTS idx_lnm_accruals_pending
    ON lnm_accruals (tenant_id, agreement_code, to_date)
    WHERE status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_lnm_provisions_pending
    ON lnm_provisions (tenant_id, agreement_code, provision_date)
    WHERE status = 'PENDING';

-- The provision table already has UNIQUE (tenant_id, agreement_code,
-- provision_date) from 20260907120100_lnm_provision_rates.sql. That
-- constraint is retained; the migration therefore performs no destructive
-- deduplication or data rewrite.

-- +goose Down
SELECT 1;
