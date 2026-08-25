-- +goose Up

ALTER TABLE business_cases
    ADD COLUMN IF NOT EXISTS idempotency_request_hash TEXT,
    ADD COLUMN IF NOT EXISTS submit_request_hash TEXT;

CREATE INDEX IF NOT EXISTS business_cases_tenant_idempotency_hash_idx
    ON business_cases (tenant_id, idempotency_key, idempotency_request_hash)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

-- +goose Down

DROP INDEX IF EXISTS business_cases_tenant_idempotency_hash_idx;
ALTER TABLE business_cases
    DROP COLUMN IF EXISTS idempotency_request_hash,
    DROP COLUMN IF EXISTS submit_request_hash;
