-- +goose Up

-- Finance tenant-owned tables must not create new rows under the historical
-- synthetic tenant. Existing rows are intentionally left for an explicit data
-- assignment/backfill gate; NOT VALID keeps that gate visible without guessing.
ALTER TABLE fin_accounts
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_accounts_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE fin_transactions
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_transactions_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE fin_approval_requests
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_approval_requests_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE fin_process_configs
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_process_configs_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE fin_account_classifications
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_account_classifications_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE fin_journal_definitions
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_journal_definitions_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE fin_regulatory_accounts
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_regulatory_accounts_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE fin_internal_accounts
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT fin_internal_accounts_tenant_id_nonempty CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;

-- +goose Down

ALTER TABLE fin_internal_accounts
    DROP CONSTRAINT IF EXISTS fin_internal_accounts_tenant_id_nonempty;
ALTER TABLE fin_regulatory_accounts
    DROP CONSTRAINT IF EXISTS fin_regulatory_accounts_tenant_id_nonempty;
ALTER TABLE fin_journal_definitions
    DROP CONSTRAINT IF EXISTS fin_journal_definitions_tenant_id_nonempty;
ALTER TABLE fin_account_classifications
    DROP CONSTRAINT IF EXISTS fin_account_classifications_tenant_id_nonempty;
ALTER TABLE fin_process_configs
    DROP CONSTRAINT IF EXISTS fin_process_configs_tenant_id_nonempty;
ALTER TABLE fin_approval_requests
    DROP CONSTRAINT IF EXISTS fin_approval_requests_tenant_id_nonempty;
ALTER TABLE fin_transactions
    DROP CONSTRAINT IF EXISTS fin_transactions_tenant_id_nonempty;
ALTER TABLE fin_accounts
    DROP CONSTRAINT IF EXISTS fin_accounts_tenant_id_nonempty;
