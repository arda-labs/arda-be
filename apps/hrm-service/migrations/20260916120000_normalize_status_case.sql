-- +goose Up
-- Status values were persisted with mixed case: the HTTP write paths used
-- lower-case ('draft', 'submitted', 'active') while the workflow gRPC
-- callbacks read and write UPPER-case ('SUBMITTED', 'APPROVED', 'ACTIVE', ...)
-- and the hrm_employee_statuses catalog seeds UPPER-case codes. UPPER-case is
-- the canonical form; normalize existing rows and align column defaults.
UPDATE hrm_employee_registrations SET status = upper(status) WHERE status <> upper(status);
UPDATE hrm_employees SET status = upper(status) WHERE status <> upper(status);
UPDATE hrm_positions SET status = upper(status) WHERE status <> upper(status);
UPDATE hrm_org_units SET status = upper(status) WHERE status <> upper(status);

ALTER TABLE hrm_employee_registrations ALTER COLUMN status SET DEFAULT 'DRAFT';
ALTER TABLE hrm_employees ALTER COLUMN status SET DEFAULT 'ACTIVE';
ALTER TABLE hrm_positions ALTER COLUMN status SET DEFAULT 'ACTIVE';
ALTER TABLE hrm_org_units ALTER COLUMN status SET DEFAULT 'ACTIVE';

-- +goose Down
-- Row values intentionally stay UPPER-case: reverting them would put the HTTP
-- write path out of sync with the workflow callbacks again.
ALTER TABLE hrm_org_units ALTER COLUMN status SET DEFAULT 'active';
ALTER TABLE hrm_positions ALTER COLUMN status SET DEFAULT 'active';
ALTER TABLE hrm_employees ALTER COLUMN status SET DEFAULT 'active';
ALTER TABLE hrm_employee_registrations ALTER COLUMN status SET DEFAULT 'draft';
