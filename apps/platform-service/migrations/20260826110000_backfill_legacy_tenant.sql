-- +goose Up
-- Only tenant-owned Platform resources are backfilled. Global parameters,
-- lookup categories and geo data intentionally keep NULL tenant_id.
UPDATE plt_organizations SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE plt_credit_institutions SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE plt_areas SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE plt_file_templates SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');

ALTER TABLE plt_organizations VALIDATE CONSTRAINT plt_organizations_tenant_id_nonempty;
ALTER TABLE plt_credit_institutions VALIDATE CONSTRAINT plt_credit_institutions_tenant_id_nonempty;
ALTER TABLE plt_areas VALIDATE CONSTRAINT plt_areas_tenant_id_nonempty;
ALTER TABLE plt_file_templates VALIDATE CONSTRAINT plt_file_templates_tenant_id_nonempty;

-- +goose Down
UPDATE plt_organizations SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE plt_credit_institutions SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE plt_areas SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE plt_file_templates SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
