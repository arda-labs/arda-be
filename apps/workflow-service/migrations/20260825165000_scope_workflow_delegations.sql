-- +goose Up
-- Delegations change effective authorization for principals and are tenant-owned.
-- Existing rows are intentionally not guessed into a tenant; operators must assign
-- them before the NOT VALID check is validated and the column is made NOT NULL.
ALTER TABLE workflow_delegations
    ADD COLUMN tenant_id VARCHAR(100);

ALTER TABLE workflow_delegations
    ADD CONSTRAINT workflow_delegations_tenant_id_nonempty
    CHECK (tenant_id IS NOT NULL AND btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;

CREATE INDEX workflow_delegations_tenant_role_idx
    ON workflow_delegations(tenant_id, role_code, effective_from DESC);

-- +goose Down
DROP INDEX IF EXISTS workflow_delegations_tenant_role_idx;
ALTER TABLE workflow_delegations
    DROP CONSTRAINT IF EXISTS workflow_delegations_tenant_id_nonempty;
ALTER TABLE workflow_delegations
    DROP COLUMN IF EXISTS tenant_id;
