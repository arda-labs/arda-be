-- +goose Up

-- Idempotency for deposit movements (P2 pilot: DPM.301 additional deposit).
-- The principal bump and the dpm_transactions insert run in one transaction;
-- the unique idempotency key makes a retry after commit a no-op instead of a
-- double credit.

ALTER TABLE dpm_transactions
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(160);

CREATE UNIQUE INDEX IF NOT EXISTS dpm_transactions_idempotency_uq
    ON dpm_transactions (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS dpm_transactions_idempotency_uq;
ALTER TABLE dpm_transactions DROP COLUMN IF EXISTS idempotency_key;
