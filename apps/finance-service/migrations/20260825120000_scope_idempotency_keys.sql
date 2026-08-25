-- +goose Up

-- Idempotency is a command concern, not a global transaction identifier.
-- The old schema used UNIQUE(idempotency_key), which caused unrelated tenants
-- and operations to collide. Keep NULL keys unrestricted and scope real keys by
-- tenant + stable operation name.
ALTER TABLE fin_transactions
    DROP CONSTRAINT IF EXISTS fin_transactions_idempotency_key_key;

DROP INDEX IF EXISTS idx_fin_txn_idempotency;

CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_txn_idempotency_scope
    ON fin_transactions (tenant_id, COALESCE(operation_name, ''), idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_fin_txn_idempotency_scope;

CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_txn_idempotency
    ON fin_transactions (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
