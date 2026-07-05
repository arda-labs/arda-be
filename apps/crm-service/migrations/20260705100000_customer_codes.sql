-- +goose Up
ALTER TABLE customers ADD COLUMN IF NOT EXISTS customer_code VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS workflow_case_id VARCHAR(64);

CREATE SEQUENCE IF NOT EXISTS crm_customer_temp_code_seq START 1;
CREATE SEQUENCE IF NOT EXISTS crm_customer_official_code_seq START 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_customer_code
    ON customers (customer_code)
    WHERE customer_code <> '';

CREATE INDEX IF NOT EXISTS idx_customers_workflow_case_id
    ON customers (workflow_case_id)
    WHERE workflow_case_id IS NOT NULL AND workflow_case_id <> '';

UPDATE customers
SET customer_code = 'DKKH-T-LEGACY-' || LPAD(nextval('crm_customer_temp_code_seq')::text, 6, '0')
WHERE customer_code = '';

-- +goose Down
DROP INDEX IF EXISTS idx_customers_workflow_case_id;
DROP INDEX IF EXISTS idx_customers_customer_code;
DROP SEQUENCE IF EXISTS crm_customer_official_code_seq;
DROP SEQUENCE IF EXISTS crm_customer_temp_code_seq;
ALTER TABLE customers DROP COLUMN IF EXISTS workflow_case_id;
ALTER TABLE customers DROP COLUMN IF EXISTS customer_code;
