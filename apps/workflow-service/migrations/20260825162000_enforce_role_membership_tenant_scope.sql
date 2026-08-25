-- +goose Up

-- Role memberships are tenant-owned data. New writes must never fall back to
-- the historical empty-string default. Existing empty rows remain an explicit
-- data-migration gate and are intentionally not assigned to a guessed tenant.
ALTER TABLE workflow_role_memberships
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT workflow_role_memberships_tenant_id_nonempty
        CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;

CREATE INDEX workflow_role_memberships_tenant_role_idx
    ON workflow_role_memberships (tenant_id, role_code, principal_type, principal_id);

-- +goose Down

DROP INDEX IF EXISTS workflow_role_memberships_tenant_role_idx;
ALTER TABLE workflow_role_memberships
    DROP CONSTRAINT IF EXISTS workflow_role_memberships_tenant_id_nonempty;
