-- +goose Up

-- P1c.4: CRM projects + customer risk flags (EPAS be_crm: inf_project,
-- project-type; customer/risk-info).

CREATE TABLE IF NOT EXISTS crm_project_types (
    id         VARCHAR(64) PRIMARY KEY,
    tenant_id  VARCHAR(64) NOT NULL DEFAULT '',
    code       VARCHAR(64) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS crm_projects (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    VARCHAR(64) NOT NULL,
    project_code VARCHAR(64) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    type_code    VARCHAR(64) NOT NULL REFERENCES crm_project_types(id),
    customer_id  VARCHAR(255) REFERENCES customers(id),
    parent_code  VARCHAR(64),
    start_date   DATE,
    end_date     DATE,
    status       VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    description  TEXT,
    org_code     VARCHAR(64),
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   TEXT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, project_code)
);

CREATE INDEX IF NOT EXISTS idx_crm_project_tenant ON crm_projects (tenant_id, org_code);

CREATE TABLE IF NOT EXISTS crm_project_members (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  VARCHAR(64) NOT NULL,
    project_id UUID NOT NULL REFERENCES crm_projects(id) ON DELETE CASCADE,
    user_id    VARCHAR(64) NOT NULL,
    role_code  VARCHAR(64),
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, project_id, user_id)
);

CREATE TABLE IF NOT EXISTS crm_customer_risk_flags (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  VARCHAR(64) NOT NULL,
    customer_id VARCHAR(255) NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    flag_code  VARCHAR(64) NOT NULL,      -- CIC | INTERNAL_LIST | WATCHLIST | MANUAL
    severity   VARCHAR(16) NOT NULL DEFAULT 'LOW', -- LOW|MEDIUM|HIGH
    note       TEXT,
    effective_date DATE NOT NULL,
    expiry_date DATE,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_crm_risk_customer ON crm_customer_risk_flags (tenant_id, customer_id);

INSERT INTO crm_project_types (id, tenant_id, code, name, created_by)
VALUES ('prj-type-lending', '00000000-0000-0000-0000-000000000010', 'LENDING', 'Dự án tín dụng', 'seed'),
       ('prj-type-infrastructure', '00000000-0000-0000-0000-000000000010', 'INFRASTRUCTURE', 'Dự án hạ tầng', 'seed')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
