-- +goose Up

-- DPM interest posting states.
--
-- Accruals are staged PENDING and only flipped POSTED (with journal_entry_id)
-- by the repository once finance confirms the GL entry; the flip and the
-- savings accrued increment happen in one transaction. Existing rows are all
-- rows written by the old code, which treated every insert as POSTED, so they
-- are intentionally left as POSTED (safe backfill: no data rewrite).
--
-- dpm_interest_ops gains the POSTING state: accrued interest has been reserved
-- and the GL posting is in flight. A crashed run resumes from POSTING and only
-- re-posts the same deterministic idempotency key.

ALTER TABLE dpm_accruals ALTER COLUMN status SET DEFAULT 'PENDING';

COMMENT ON COLUMN dpm_accruals.status IS
    'PENDING = staged with GL pending, POSTED = GL confirmed and accrued balance applied';

COMMENT ON COLUMN dpm_interest_ops.status IS
    'DRAFT|SUBMITTED|POSTING|POSTED|REJECTED (POSTING = accrued reserved, GL posting in flight)';

-- Speeds up the per-savings oldest-unfinished-accrual retry lookup.
CREATE INDEX IF NOT EXISTS idx_dpm_accrual_pending
    ON dpm_accruals (tenant_id, savings_id, period_to)
    WHERE status <> 'POSTED';

-- +goose Down

DROP INDEX IF EXISTS idx_dpm_accrual_pending;

COMMENT ON COLUMN dpm_interest_ops.status IS 'DRAFT|SUBMITTED|POSTED|REJECTED';
COMMENT ON COLUMN dpm_accruals.status IS NULL;

ALTER TABLE dpm_accruals ALTER COLUMN status SET DEFAULT 'POSTED';
