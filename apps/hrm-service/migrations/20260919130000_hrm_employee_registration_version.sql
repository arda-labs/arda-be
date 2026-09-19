-- +goose Up
-- Row version for the employee-registration maker-checker case: the checker
-- form reads it from GET /api/hrm/employee-registrations/{id} and sends it back
-- so the domain can refuse a stale approval.
ALTER TABLE hrm_employee_registrations ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE hrm_employee_registrations DROP COLUMN IF EXISTS version;
