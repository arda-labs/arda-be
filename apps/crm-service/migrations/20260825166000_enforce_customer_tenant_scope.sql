-- +goose Up
-- Customer rows are tenant-owned. The historical 'default' value is a
-- placeholder and must be assigned by the data owner before validation.
ALTER TABLE customers
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT customers_tenant_id_nonempty
    CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;

-- +goose Down
ALTER TABLE customers
    DROP CONSTRAINT IF EXISTS customers_tenant_id_nonempty;
