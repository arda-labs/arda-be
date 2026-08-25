-- +goose Up
-- Historical bootstrap rows may still use the reserved legacy tenant `default`.
-- They are intentionally not reassigned by this migration. New IAM writes must
-- provide an explicit tenant and may not recreate that placeholder value.

ALTER TABLE iam_users ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE iam_roles ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE iam_groups ALTER COLUMN tenant_id DROP DEFAULT;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'iam_users'::regclass
          AND conname = 'iam_users_explicit_tenant_ck'
    ) THEN
        ALTER TABLE iam_users
            ADD CONSTRAINT iam_users_explicit_tenant_ck
            CHECK (btrim(tenant_id) <> '' AND tenant_id <> 'default') NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'iam_roles'::regclass
          AND conname = 'iam_roles_explicit_tenant_ck'
    ) THEN
        ALTER TABLE iam_roles
            ADD CONSTRAINT iam_roles_explicit_tenant_ck
            CHECK (btrim(tenant_id) <> '' AND tenant_id <> 'default') NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'iam_groups'::regclass
          AND conname = 'iam_groups_explicit_tenant_ck'
    ) THEN
        ALTER TABLE iam_groups
            ADD CONSTRAINT iam_groups_explicit_tenant_ck
            CHECK (btrim(tenant_id) <> '' AND tenant_id <> 'default') NOT VALID;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE iam_users DROP CONSTRAINT IF EXISTS iam_users_explicit_tenant_ck;
ALTER TABLE iam_roles DROP CONSTRAINT IF EXISTS iam_roles_explicit_tenant_ck;
ALTER TABLE iam_groups DROP CONSTRAINT IF EXISTS iam_groups_explicit_tenant_ck;
-- Do not restore a synthetic tenant default during rollback.
