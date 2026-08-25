-- +goose Up
-- Existing rows must be assigned by an explicit data-owner migration before
-- this schema change is deployed. There is intentionally no synthetic tenant
-- fallback because that would merge unrelated HRM data.
DO $$
DECLARE
    table_name text;
    remaining bigint;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'hrm_positions',
        'hrm_job_titles',
        'hrm_org_units',
        'hrm_employees',
        'hrm_employee_registrations'
    ] LOOP
        EXECUTE format('SELECT count(*) FROM %I', table_name) INTO remaining;
        IF remaining > 0 THEN
            RAISE EXCEPTION 'HRM tenant migration requires explicit backfill for table %; found % rows', table_name, remaining;
        END IF;
    END LOOP;
END $$;

ALTER TABLE hrm_positions ADD COLUMN tenant_id text;
ALTER TABLE hrm_job_titles ADD COLUMN tenant_id text;
ALTER TABLE hrm_org_units ADD COLUMN tenant_id text;
ALTER TABLE hrm_employees ADD COLUMN tenant_id text;
ALTER TABLE hrm_employee_registrations ADD COLUMN tenant_id text;

ALTER TABLE hrm_positions ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE hrm_job_titles ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE hrm_org_units ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE hrm_employees ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE hrm_employee_registrations ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE hrm_positions DROP CONSTRAINT IF EXISTS hrm_positions_code_key;
ALTER TABLE hrm_job_titles DROP CONSTRAINT IF EXISTS hrm_job_titles_code_key;
ALTER TABLE hrm_org_units DROP CONSTRAINT IF EXISTS hrm_org_units_code_key;
ALTER TABLE hrm_employees DROP CONSTRAINT IF EXISTS hrm_employees_employee_code_key;
ALTER TABLE hrm_employee_registrations DROP CONSTRAINT IF EXISTS hrm_employee_registrations_registration_code_key;

CREATE UNIQUE INDEX hrm_positions_tenant_code_uq ON hrm_positions(tenant_id, code);
CREATE UNIQUE INDEX hrm_job_titles_tenant_code_uq ON hrm_job_titles(tenant_id, code);
CREATE UNIQUE INDEX hrm_org_units_tenant_code_uq ON hrm_org_units(tenant_id, code);
CREATE UNIQUE INDEX hrm_employees_tenant_code_uq ON hrm_employees(tenant_id, employee_code);
CREATE UNIQUE INDEX hrm_employee_registrations_tenant_code_uq ON hrm_employee_registrations(tenant_id, registration_code);

CREATE INDEX hrm_positions_tenant_idx ON hrm_positions(tenant_id);
CREATE INDEX hrm_job_titles_tenant_idx ON hrm_job_titles(tenant_id);
CREATE INDEX hrm_org_units_tenant_idx ON hrm_org_units(tenant_id);
CREATE INDEX hrm_employees_tenant_idx ON hrm_employees(tenant_id);
CREATE INDEX hrm_employee_registrations_tenant_idx ON hrm_employee_registrations(tenant_id);

-- +goose Down
DROP INDEX IF EXISTS hrm_employee_registrations_tenant_idx;
DROP INDEX IF EXISTS hrm_employees_tenant_idx;
DROP INDEX IF EXISTS hrm_org_units_tenant_idx;
DROP INDEX IF EXISTS hrm_job_titles_tenant_idx;
DROP INDEX IF EXISTS hrm_positions_tenant_idx;
DROP INDEX IF EXISTS hrm_employee_registrations_tenant_code_uq;
DROP INDEX IF EXISTS hrm_employees_tenant_code_uq;
DROP INDEX IF EXISTS hrm_org_units_tenant_code_uq;
DROP INDEX IF EXISTS hrm_job_titles_tenant_code_uq;
DROP INDEX IF EXISTS hrm_positions_tenant_code_uq;
ALTER TABLE hrm_employee_registrations DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE hrm_employees DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE hrm_org_units DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE hrm_job_titles DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE hrm_positions DROP COLUMN IF EXISTS tenant_id;
