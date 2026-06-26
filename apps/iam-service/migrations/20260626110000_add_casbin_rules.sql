-- +goose Up

-- Casbin policy rules table
CREATE TABLE IF NOT EXISTS iam_casbin_rules (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    ptype TEXT NOT NULL DEFAULT '',
    v0 TEXT NOT NULL DEFAULT '',
    v1 TEXT NOT NULL DEFAULT '',
    v2 TEXT NOT NULL DEFAULT '',
    v3 TEXT NOT NULL DEFAULT '',
    v4 TEXT NOT NULL DEFAULT '',
    v5 TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_casbin_ptype ON iam_casbin_rules(ptype);

-- Seed RBAC: role → permission mappings using Casbin format (g, role, permission)
-- g = grouping policy: g <user_or_role> <role>
-- p = policy: p <sub> <obj> <act> <eft>

-- Role hierarchy: ADMIN > MANAGER > USER
INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3) VALUES
    ('g', 'ADMIN', 'MANAGER', '', ''),
    ('g', 'MANAGER', 'USER', '', ''),
    ('g', 'SYSTEM', 'ADMIN', '', '');

-- ── FINANCE permissions ──
INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3) VALUES
    ('p', 'USER', 'finance:account', 'read', 'allow'),
    ('p', 'MANAGER', 'finance:account', 'write', 'allow'),
    ('p', 'MANAGER', 'finance:transaction', 'read', 'allow'),
    ('p', 'MANAGER', 'finance:transaction', 'create', 'allow'),
    ('p', 'ADMIN', 'finance:*', '*', 'allow');

-- ── CRM permissions ──
INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3) VALUES
    ('p', 'USER', 'crm:customer', 'read', 'allow'),
    ('p', 'MANAGER', 'crm:customer', 'write', 'allow'),
    ('p', 'ADMIN', 'crm:*', '*', 'allow');

-- ── IAM permissions ──
INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3) VALUES
    ('p', 'USER', 'iam:user', 'read', 'allow'),
    ('p', 'MANAGER', 'iam:user', 'write', 'allow'),
    ('p', 'ADMIN', 'iam:*', '*', 'allow');

-- ── Tenant specific ──
INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3) VALUES
    ('p', 'SYSTEM', 'tenant:*', '*', 'allow');

-- +goose Down
DROP TABLE IF EXISTS iam_casbin_rules CASCADE;
