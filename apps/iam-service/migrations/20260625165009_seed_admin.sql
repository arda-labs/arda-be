-- +goose Up
INSERT INTO iam_organizations (id, code, name, status)
VALUES ('00000000-0000-0000-0000-000000000001', 'root', 'Root Organization', 'ACTIVE')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_roles (id, code, name, status)
VALUES ('00000000-0000-0000-0000-000000000001', 'ADMIN', 'Administrator', 'ACTIVE')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    ('00000000-0000-0000-0000-000000000001', 'iam.user.read', 'Read users', 'iam', 'user', 'read'),
    ('00000000-0000-0000-0000-000000000002', 'iam.user.create', 'Create users', 'iam', 'user', 'create'),
    ('00000000-0000-0000-0000-000000000003', 'iam.user.update', 'Update users', 'iam', 'user', 'update'),
    ('00000000-0000-0000-0000-000000000004', 'iam.user.delete', 'Delete users', 'iam', 'user', 'delete'),
    ('00000000-0000-0000-0000-000000000005', 'iam.role.read', 'Read roles', 'iam', 'role', 'read'),
    ('00000000-0000-0000-0000-000000000006', 'iam.role.create', 'Create roles', 'iam', 'role', 'create'),
    ('00000000-0000-0000-0000-000000000007', 'iam.role.update', 'Update roles', 'iam', 'role', 'update'),
    ('00000000-0000-0000-0000-000000000008', 'iam.role.delete', 'Delete roles', 'iam', 'role', 'delete'),
    ('00000000-0000-0000-0000-000000000009', 'iam.permission.read', 'Read permissions', 'iam', 'permission', 'read'),
    ('00000000-0000-0000-0000-00000000000a', 'iam.permission.create', 'Create permissions', 'iam', 'permission', 'create'),
    ('00000000-0000-0000-0000-00000000000b', 'iam.user.assign_role', 'Assign roles to users', 'iam', 'user', 'assign_role'),
    ('00000000-0000-0000-0000-00000000000c', 'iam.role.assign_permission', 'Assign permissions to roles', 'iam', 'role', 'assign_permission')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_users (id, external_subject, username, email, display_name, status, tenant_id)
VALUES ('00000000-0000-0000-0000-000000000001', 'dev-admin-sub', 'admin', 'admin@arda.local', 'System Admin', 'ACTIVE', 'default')
ON CONFLICT (username) DO NOTHING;

INSERT INTO iam_user_organizations (user_id, organization_id)
VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001')
ON CONFLICT DO NOTHING;

INSERT INTO iam_user_roles (user_id, role_id)
VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001')
ON CONFLICT DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT '00000000-0000-0000-0000-000000000001', id FROM iam_permissions
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_role_permissions WHERE role_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM iam_user_roles WHERE user_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM iam_user_organizations WHERE user_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM iam_permissions WHERE id LIKE '00000000-0000-0000-0000-0000000000%';
DELETE FROM iam_users WHERE id = '00000000-0000-0000-0000-000000000001';
DELETE FROM iam_roles WHERE id = '00000000-0000-0000-0000-000000000001';
DELETE FROM iam_organizations WHERE id = '00000000-0000-0000-0000-000000000001';
