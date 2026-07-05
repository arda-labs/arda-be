-- +goose Up
ALTER TABLE fin_transactions
    ADD COLUMN IF NOT EXISTS reversed_transaction_id UUID REFERENCES fin_transactions(id);

CREATE INDEX IF NOT EXISTS idx_fin_txn_reversed
    ON fin_transactions(reversed_transaction_id)
    WHERE reversed_transaction_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_fin_txn_reversed;
ALTER TABLE fin_transactions DROP COLUMN IF EXISTS reversed_transaction_id;
