-- +goose Up
-- Business cases and workflow memberships were historically single-tenant.
-- Delegations received their tenant column during the refactor and therefore
-- need the same explicit assignment before the constraint is validated.
UPDATE business_cases SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE workflow_role_memberships SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE workflow_delegations SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE tenant_id IS NULL OR lower(btrim(tenant_id)) IN ('', 'default');

ALTER TABLE workflow_role_memberships VALIDATE CONSTRAINT workflow_role_memberships_tenant_id_nonempty;
ALTER TABLE workflow_delegations VALIDATE CONSTRAINT workflow_delegations_tenant_id_nonempty;
ALTER TABLE workflow_delegations ALTER COLUMN tenant_id SET NOT NULL;

-- +goose Down
UPDATE business_cases SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE workflow_role_memberships SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE workflow_delegations SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
ALTER TABLE workflow_delegations ALTER COLUMN tenant_id DROP NOT NULL;
