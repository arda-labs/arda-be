-- +goose Up

-- Platform tenant-owned resources must not create new rows under the old
-- synthetic tenant default. Existing rows remain a deliberate data-assignment
-- gate and are not silently reassigned.
ALTER TABLE plt_organizations
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT plt_organizations_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE plt_credit_institutions
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT plt_credit_institutions_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE plt_areas
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT plt_areas_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE plt_file_templates
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT plt_file_templates_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;

-- +goose Down

ALTER TABLE plt_file_templates
    DROP CONSTRAINT IF EXISTS plt_file_templates_tenant_id_nonempty;
ALTER TABLE plt_areas
    DROP CONSTRAINT IF EXISTS plt_areas_tenant_id_nonempty;
ALTER TABLE plt_credit_institutions
    DROP CONSTRAINT IF EXISTS plt_credit_institutions_tenant_id_nonempty;
ALTER TABLE plt_organizations
    DROP CONSTRAINT IF EXISTS plt_organizations_tenant_id_nonempty;
