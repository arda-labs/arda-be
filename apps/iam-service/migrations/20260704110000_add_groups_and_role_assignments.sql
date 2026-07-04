-- +goose Up
ALTER TABLE iam_roles
  ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(64) NOT NULL DEFAULT 'default';

CREATE TABLE IF NOT EXISTS iam_groups (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    code VARCHAR(128) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    is_system BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS iam_group_members (
    group_id UUID NOT NULL REFERENCES iam_groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_iam_group_members_user ON iam_group_members(user_id);

CREATE TABLE IF NOT EXISTS iam_role_assignments (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    principal_type VARCHAR(32) NOT NULL,
    principal_id UUID NOT NULL,
    role_id UUID NOT NULL REFERENCES iam_roles(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL DEFAULT 'global',
    scope_id VARCHAR(128),
    effect VARCHAR(16) NOT NULL DEFAULT 'allow',
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    effective_from TIMESTAMPTZ,
    effective_to TIMESTAMPTZ,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (principal_type IN ('USER', 'GROUP')),
    CHECK (effect = 'allow')
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_iam_role_assignments_unique
    ON iam_role_assignments (principal_type, principal_id, role_id, scope_type, COALESCE(scope_id, ''));
CREATE INDEX IF NOT EXISTS idx_iam_role_assignments_role ON iam_role_assignments(role_id);
CREATE INDEX IF NOT EXISTS idx_iam_role_assignments_principal ON iam_role_assignments(principal_type, principal_id);

INSERT INTO iam_role_assignments (principal_type, principal_id, role_id, scope_type)
SELECT 'USER', user_id, role_id, 'global'
FROM iam_user_roles
ON CONFLICT DO NOTHING;

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'iam.group.read', 'Read IAM groups', 'iam', 'group', 'read'),
    (uuidv7(), 'iam.group.manage', 'Manage IAM groups', 'iam', 'group', 'manage'),
    (uuidv7(), 'iam.role.assign', 'Assign IAM roles', 'iam', 'role', 'assign')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'hrm.read', 'Read HRM data', 'hrm', '*', 'read'),
    (uuidv7(), 'hrm.manage', 'Manage HRM data', 'hrm', '*', 'manage'),
    (uuidv7(), 'hrm.registration.submit', 'Submit HRM employee registrations', 'hrm', 'registration', 'submit'),
    (uuidv7(), 'hrm.registration.review', 'Review HRM employee registrations', 'hrm', 'registration', 'review'),
    (uuidv7(), 'hrm.registration.approve', 'Approve HRM employee registrations', 'hrm', 'registration', 'approve')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_roles (id, code, name, status, tenant_id)
VALUES
    (uuidv7(), 'HRM_REGISTRATION_SUBMITTER', 'HRM registration submitter', 'ACTIVE', 'default'),
    (uuidv7(), 'HRM_REGISTRATION_REVIEWER', 'HRM registration reviewer', 'ACTIVE', 'default'),
    (uuidv7(), 'HRM_REGISTRATION_APPROVER', 'HRM registration approver', 'ACTIVE', 'default')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_groups (id, code, name, description, status, tenant_id, is_system)
VALUES
    (uuidv7(), 'HRM_REVIEWERS', 'HRM reviewers', 'Users who can review HRM employee registrations', 'ACTIVE', 'default', false),
    (uuidv7(), 'HRM_APPROVERS', 'HRM approvers', 'Users who can approve HRM employee registrations', 'ACTIVE', 'default', false)
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON (
    (r.code = 'HRM_REGISTRATION_SUBMITTER' AND p.code IN ('hrm.read', 'hrm.registration.submit')) OR
    (r.code = 'HRM_REGISTRATION_REVIEWER' AND p.code IN ('hrm.read', 'hrm.registration.review')) OR
    (r.code = 'HRM_REGISTRATION_APPROVER' AND p.code IN ('hrm.read', 'hrm.registration.approve'))
)
ON CONFLICT DO NOTHING;

INSERT INTO iam_role_assignments (principal_type, principal_id, role_id, scope_type)
SELECT 'GROUP', g.id, r.id, 'global'
FROM iam_groups g
JOIN iam_roles r ON (
    (g.code = 'HRM_REVIEWERS' AND r.code = 'HRM_REGISTRATION_REVIEWER') OR
    (g.code = 'HRM_APPROVERS' AND r.code = 'HRM_REGISTRATION_APPROVER')
)
ON CONFLICT DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN (
    'iam.group.read',
    'iam.group.manage',
    'iam.role.assign',
    'hrm.read',
    'hrm.manage',
    'hrm.registration.submit',
    'hrm.registration.review',
    'hrm.registration.approve'
)
WHERE r.code = 'SUPER_ADMIN'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_role_permissions
WHERE permission_id IN (
    SELECT id FROM iam_permissions
    WHERE code IN (
        'iam.group.read',
        'iam.group.manage',
        'iam.role.assign',
        'hrm.read',
        'hrm.manage',
        'hrm.registration.submit',
        'hrm.registration.review',
        'hrm.registration.approve'
    )
);
DELETE FROM iam_permissions
WHERE code IN (
    'iam.group.read',
    'iam.group.manage',
    'iam.role.assign',
    'hrm.read',
    'hrm.manage',
    'hrm.registration.submit',
    'hrm.registration.review',
    'hrm.registration.approve'
);
DELETE FROM iam_role_assignments
WHERE role_id IN (
    SELECT id FROM iam_roles
    WHERE code IN ('HRM_REGISTRATION_SUBMITTER', 'HRM_REGISTRATION_REVIEWER', 'HRM_REGISTRATION_APPROVER')
);
DELETE FROM iam_role_permissions
WHERE role_id IN (
    SELECT id FROM iam_roles
    WHERE code IN ('HRM_REGISTRATION_SUBMITTER', 'HRM_REGISTRATION_REVIEWER', 'HRM_REGISTRATION_APPROVER')
);
DELETE FROM iam_roles
WHERE code IN ('HRM_REGISTRATION_SUBMITTER', 'HRM_REGISTRATION_REVIEWER', 'HRM_REGISTRATION_APPROVER');
DELETE FROM iam_groups
WHERE code IN ('HRM_REVIEWERS', 'HRM_APPROVERS');
DROP TABLE IF EXISTS iam_role_assignments;
DROP TABLE IF EXISTS iam_group_members;
DROP TABLE IF EXISTS iam_groups;
