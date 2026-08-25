-- +goose Up
-- The historical CRM database had one implicit tenant represented by
-- 'default'. The IAM migration registers that business tenant explicitly.
UPDATE customers
SET tenant_id = '00000000-0000-0000-0000-000000000010'
WHERE lower(btrim(tenant_id)) IN ('', 'default');

ALTER TABLE customers
    VALIDATE CONSTRAINT customers_tenant_id_nonempty;

-- +goose Down
UPDATE customers
SET tenant_id = 'default'
WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
