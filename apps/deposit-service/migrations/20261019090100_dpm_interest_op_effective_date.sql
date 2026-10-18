-- +goose Up

-- Business date on which a PAY / CAPITALIZE op takes effect.
--
-- The interest period of a savings starts at the open date, or at the
-- effective_date of its last PAY/CAPITALIZE op that is POSTING or POSTED; the
-- cumulative daily accrual rounds per period (T9.1, owner decision of
-- 2026-10-18). Before this column the op carried no business date (only
-- created_at, wall clock). Deriving the period start from the ops keeps a
-- single source of truth: a compensated op (back to SUBMITTED) drops out of
-- the derivation by itself.
--
-- Backfill: POSTING/POSTED ops get created_at converted to Asia/Ho_Chi_Minh,
-- an estimate (the op was approved some time after it was created). To review
-- the estimated rows:
--
--   SELECT savings_code, op_type, status, created_at, effective_date
--   FROM dpm_interest_ops
--   WHERE status IN ('POSTING', 'POSTED') AND op_type IN ('PAY', 'CAPITALIZE')
--   ORDER BY savings_code, created_at;

ALTER TABLE dpm_interest_ops ADD COLUMN IF NOT EXISTS effective_date DATE;

UPDATE dpm_interest_ops
SET effective_date = (created_at AT TIME ZONE 'Asia/Ho_Chi_Minh')::date
WHERE effective_date IS NULL AND status IN ('POSTING', 'POSTED');

ALTER TABLE dpm_interest_ops
    ADD CONSTRAINT chk_dpm_interest_op_effective_date
    CHECK (status NOT IN ('POSTING', 'POSTED') OR effective_date IS NOT NULL);

COMMENT ON COLUMN dpm_interest_ops.effective_date IS
    'Business date the op takes effect; NOT NULL once POSTING/POSTED; starts a new interest period for PAY and CAPITALIZE';

CREATE INDEX IF NOT EXISTS idx_dpm_interest_op_period
    ON dpm_interest_ops (tenant_id, savings_id, effective_date)
    WHERE op_type IN ('PAY', 'CAPITALIZE') AND status IN ('POSTING', 'POSTED');

-- +goose Down

DROP INDEX IF EXISTS idx_dpm_interest_op_period;

ALTER TABLE dpm_interest_ops
    DROP CONSTRAINT IF EXISTS chk_dpm_interest_op_effective_date,
    DROP COLUMN IF EXISTS effective_date;
