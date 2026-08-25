-- +goose Up

ALTER TABLE fin_transactions
    ADD COLUMN IF NOT EXISTS request_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_fin_txn_idempotency_hash
    ON fin_transactions (tenant_id, COALESCE(operation_name, ''), idempotency_key, request_hash)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_fin_txn_idempotency_hash;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS request_hash;
