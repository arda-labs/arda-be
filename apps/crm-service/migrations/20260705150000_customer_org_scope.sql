-- +goose Up
ALTER TABLE customers ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(64) NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS idx_customers_tenant_org_status
    ON customers(tenant_id, org_id, status);

CREATE INDEX IF NOT EXISTS idx_customers_tenant_org_identity
    ON customers(tenant_id, org_id, identity_no)
    WHERE identity_no <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_customers_tenant_org_identity;
DROP INDEX IF EXISTS idx_customers_tenant_org_status;
ALTER TABLE customers DROP COLUMN IF EXISTS tenant_id;
