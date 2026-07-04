-- +goose Up
CREATE TABLE IF NOT EXISTS hrm_positions (
    id text PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    is_manager boolean NOT NULL DEFAULT false,
    description text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hrm_job_titles (
    id text PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    description text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hrm_org_units (
    id text PRIMARY KEY,
    code text NOT NULL UNIQUE,
    organization_id text NOT NULL,
    name text NOT NULL,
    org_level text NOT NULL,
    parent_id text REFERENCES hrm_org_units(id),
    department_type text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    description text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS hrm_org_units_organization_idx ON hrm_org_units(organization_id);
CREATE INDEX IF NOT EXISTS hrm_org_units_parent_idx ON hrm_org_units(parent_id);

CREATE TABLE IF NOT EXISTS hrm_employees (
    id text PRIMARY KEY,
    employee_code text NOT NULL UNIQUE,
    full_name text NOT NULL,
    org_unit_id text REFERENCES hrm_org_units(id),
    position_id text REFERENCES hrm_positions(id),
    job_title_id text REFERENCES hrm_job_titles(id),
    iam_user_id text,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hrm_employee_registrations (
    id text PRIMARY KEY,
    registration_code text NOT NULL UNIQUE,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    workflow_case_id text,
    status text NOT NULL DEFAULT 'draft',
    created_by text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS hrm_employee_registrations_status_idx ON hrm_employee_registrations(status, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS hrm_employee_registrations;
DROP TABLE IF EXISTS hrm_employees;
DROP TABLE IF EXISTS hrm_org_units;
DROP TABLE IF EXISTS hrm_job_titles;
DROP TABLE IF EXISTS hrm_positions;
