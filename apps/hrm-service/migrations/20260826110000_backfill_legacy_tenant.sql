-- +goose Up
-- HRM's tenant columns were introduced after the legacy tables existed. All
-- legacy rows belong to the original Arda business tenant; there is no safe
-- per-row tenant signal in the old schema.
UPDATE hrm_positions SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE tenant_id IS NULL OR lower(btrim(tenant_id)) IN ('', 'default');
UPDATE hrm_job_titles SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE tenant_id IS NULL OR lower(btrim(tenant_id)) IN ('', 'default');
UPDATE hrm_org_units SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE tenant_id IS NULL OR lower(btrim(tenant_id)) IN ('', 'default');
UPDATE hrm_employees SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE tenant_id IS NULL OR lower(btrim(tenant_id)) IN ('', 'default');
UPDATE hrm_employee_registrations SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE tenant_id IS NULL OR lower(btrim(tenant_id)) IN ('', 'default');

-- +goose Down
UPDATE hrm_positions SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE hrm_job_titles SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE hrm_org_units SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE hrm_employees SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE hrm_employee_registrations SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
