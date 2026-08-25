-- +goose Up
ALTER TABLE business_cases ADD COLUMN idempotency_key text;
ALTER TABLE business_cases ADD COLUMN submit_idempotency_key text;

ALTER TABLE business_cases DROP CONSTRAINT IF EXISTS business_cases_case_code_key;
CREATE UNIQUE INDEX business_cases_tenant_case_code_uq
    ON business_cases(tenant_id, case_code);
CREATE UNIQUE INDEX business_cases_tenant_idempotency_uq
    ON business_cases(tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
CREATE UNIQUE INDEX business_cases_tenant_submit_idempotency_uq
    ON business_cases(tenant_id, submit_idempotency_key)
    WHERE submit_idempotency_key IS NOT NULL AND submit_idempotency_key <> '';

-- +goose Down
DROP INDEX IF EXISTS business_cases_tenant_submit_idempotency_uq;
DROP INDEX IF EXISTS business_cases_tenant_idempotency_uq;
DROP INDEX IF EXISTS business_cases_tenant_case_code_uq;
ALTER TABLE business_cases DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE business_cases DROP COLUMN IF EXISTS submit_idempotency_key;
