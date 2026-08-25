-- +goose Up

-- Tenant IDs are opaque application identifiers. New IDs are generated as
-- UUIDv7 strings, while the varchar representation keeps the contract
-- compatible with the existing service databases and protobuf messages.
CREATE TABLE IF NOT EXISTS iam_tenants (
    id VARCHAR(64) PRIMARY KEY DEFAULT uuidv7()::text,
    code VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (btrim(id) <> '' AND lower(btrim(id)) <> 'default'),
    CHECK (btrim(code) <> '' AND lower(btrim(code)) <> 'default'),
    CHECK (status IN ('PROVISIONING', 'ACTIVE', 'SUSPENDED', 'DELETING'))
);

CREATE TABLE IF NOT EXISTS iam_tenant_memberships (
    tenant_id VARCHAR(64) NOT NULL REFERENCES iam_tenants(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id),
    CHECK (status IN ('INVITED', 'ACTIVE', 'SUSPENDED', 'REMOVED'))
);
CREATE INDEX IF NOT EXISTS idx_iam_tenant_memberships_user
    ON iam_tenant_memberships(user_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_iam_tenant_memberships_one_default
    ON iam_tenant_memberships(user_id)
    WHERE is_default AND status = 'ACTIVE';

-- The first business tenant is deliberately explicit. The reserved `system`
-- scope remains outside this registry for control-plane superadmin access.
INSERT INTO iam_tenants (id, code, name, status)
VALUES ('00000000-0000-0000-0000-000000000010', 'arda-internal', 'Arda Internal', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET
    code = EXCLUDED.code,
    name = EXCLUDED.name,
    status = EXCLUDED.status,
    updated_at = now();

-- Convert only the known legacy placeholder. Do not infer a tenant from an
-- email address or from an organization name.
UPDATE iam_users
SET tenant_id = '00000000-0000-0000-0000-000000000010', updated_at = now()
WHERE lower(btrim(tenant_id)) IN ('', 'default')
  AND username <> 'superadmin';

UPDATE iam_roles
SET tenant_id = '00000000-0000-0000-0000-000000000010', updated_at = now()
WHERE lower(btrim(tenant_id)) IN ('', 'default')
  AND code <> 'SUPER_ADMIN';

UPDATE iam_groups
SET tenant_id = '00000000-0000-0000-0000-000000000010', updated_at = now()
WHERE lower(btrim(tenant_id)) IN ('', 'default');

ALTER TABLE iam_users VALIDATE CONSTRAINT iam_users_explicit_tenant_ck;
ALTER TABLE iam_roles VALIDATE CONSTRAINT iam_roles_explicit_tenant_ck;
ALTER TABLE iam_groups VALIDATE CONSTRAINT iam_groups_explicit_tenant_ck;

-- Preserve existing non-placeholder tenant identifiers by registering them as
-- explicit tenants. This is a deterministic catalog migration, not a fallback.
INSERT INTO iam_tenants (id, code, name, status)
SELECT DISTINCT tenant_id, tenant_id, tenant_id, 'ACTIVE'
FROM (
    SELECT tenant_id FROM iam_users
    UNION
    SELECT tenant_id FROM iam_roles
    UNION
    SELECT tenant_id FROM iam_groups
) legacy
WHERE btrim(tenant_id) <> ''
  AND lower(btrim(tenant_id)) NOT IN ('default', 'system')
ON CONFLICT (id) DO NOTHING;

-- Bind the existing root organization to the first business tenant.
ALTER TABLE iam_organizations
    ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(64);

UPDATE iam_organizations
SET tenant_id = '00000000-0000-0000-0000-000000000010', updated_at = now()
WHERE tenant_id IS NULL;

INSERT INTO iam_tenants (id, code, name, status)
SELECT DISTINCT tenant_id, tenant_id, tenant_id, 'ACTIVE'
FROM iam_organizations
WHERE tenant_id IS NOT NULL
  AND btrim(tenant_id) <> ''
  AND lower(btrim(tenant_id)) NOT IN ('default', 'system')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE iam_organizations
    DROP CONSTRAINT IF EXISTS iam_organizations_code_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_iam_organizations_tenant_code
    ON iam_organizations(tenant_id, code);
ALTER TABLE iam_organizations
    ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE iam_organizations
    ADD CONSTRAINT iam_organizations_tenant_fk
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

-- Tenant-local role/group names may repeat across tenants.
ALTER TABLE iam_roles DROP CONSTRAINT IF EXISTS iam_roles_code_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_iam_roles_tenant_code
    ON iam_roles(tenant_id, code);
ALTER TABLE iam_groups DROP CONSTRAINT IF EXISTS iam_groups_code_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_iam_groups_tenant_code
    ON iam_groups(tenant_id, code);

INSERT INTO iam_tenant_memberships (tenant_id, user_id, status, is_default)
SELECT u.tenant_id, u.id, 'ACTIVE', true
FROM iam_users u
WHERE btrim(u.tenant_id) <> ''
  AND lower(btrim(u.tenant_id)) NOT IN ('default', 'system')
ON CONFLICT (tenant_id, user_id) DO UPDATE SET
    status = 'ACTIVE',
    is_default = EXCLUDED.is_default,
    updated_at = now();

-- +goose Down
DROP TABLE IF EXISTS iam_tenant_memberships;
DROP TABLE IF EXISTS iam_tenants;
