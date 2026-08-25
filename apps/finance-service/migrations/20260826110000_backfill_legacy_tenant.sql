-- +goose Up
-- Move the single legacy business scope to the explicit Arda internal tenant.
UPDATE fin_accounts SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE fin_transactions SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE fin_approval_requests SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE fin_process_configs SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE fin_account_classifications SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE fin_journal_definitions SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE fin_regulatory_accounts SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE fin_internal_accounts SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');

ALTER TABLE fin_accounts VALIDATE CONSTRAINT fin_accounts_tenant_id_nonempty;
ALTER TABLE fin_transactions VALIDATE CONSTRAINT fin_transactions_tenant_id_nonempty;
ALTER TABLE fin_approval_requests VALIDATE CONSTRAINT fin_approval_requests_tenant_id_nonempty;
ALTER TABLE fin_process_configs VALIDATE CONSTRAINT fin_process_configs_tenant_id_nonempty;
ALTER TABLE fin_account_classifications VALIDATE CONSTRAINT fin_account_classifications_tenant_id_nonempty;
ALTER TABLE fin_journal_definitions VALIDATE CONSTRAINT fin_journal_definitions_tenant_id_nonempty;
ALTER TABLE fin_regulatory_accounts VALIDATE CONSTRAINT fin_regulatory_accounts_tenant_id_nonempty;
ALTER TABLE fin_internal_accounts VALIDATE CONSTRAINT fin_internal_accounts_tenant_id_nonempty;

-- +goose Down
UPDATE fin_accounts SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE fin_transactions SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE fin_approval_requests SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE fin_process_configs SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE fin_account_classifications SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE fin_journal_definitions SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE fin_regulatory_accounts SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE fin_internal_accounts SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
