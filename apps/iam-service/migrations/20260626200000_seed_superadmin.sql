-- +goose Up

-- SUPER_ADMIN role (học theo pattern seed admin gốc, không dùng tenant_id)
INSERT INTO iam_roles (id, code, name, status)
VALUES ('00000000-0000-0000-0000-000000000002', 'SUPER_ADMIN', 'Super Administrator', 'ACTIVE')
ON CONFLICT (code) DO NOTHING;

-- Gán tất cả permissions hiện có cho SUPER_ADMIN
INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT '00000000-0000-0000-0000-000000000002', id FROM iam_permissions
ON CONFLICT DO NOTHING;

-- Thêm sentinel permission "superadmin" — dùng làm wildcard bypass ở permission check
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES ('00000000-0000-0000-0000-00000000000d', 'superadmin', 'Super Admin Access (wildcard)', 'system', '*', '*')
ON CONFLICT (code) DO NOTHING;

-- Gán sentinel permission cho SUPER_ADMIN role
INSERT INTO iam_role_permissions (role_id, permission_id)
VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-00000000000d')
ON CONFLICT DO NOTHING;

-- Super admin user is system-owned. Password is set only by iam-service
-- bootstrap from SUPERADMIN_INITIAL_PASSWORD or SUPERADMIN_PASSWORD_HASH.
INSERT INTO iam_users (id, external_subject, username, email, display_name, password_hash, source, status, tenant_id)
VALUES (
    '00000000-0000-0000-0000-000000000002',
    'super-admin',
    'superadmin',
    'superadmin@arda.local',
    'Super Admin',
    '',
    'internal',
    'LOCKED',
    'default'
)
ON CONFLICT (username) DO NOTHING;

-- Gán SUPER_ADMIN role
INSERT INTO iam_user_roles (user_id, role_id)
VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000002')
ON CONFLICT DO NOTHING;

-- Thêm vào Root Organization
INSERT INTO iam_user_organizations (user_id, organization_id)
VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_user_organizations WHERE user_id = '00000000-0000-0000-0000-000000000002';
DELETE FROM iam_user_roles WHERE user_id = '00000000-0000-0000-0000-000000000002';
DELETE FROM iam_role_permissions WHERE permission_id = '00000000-0000-0000-0000-00000000000d';
DELETE FROM iam_permissions WHERE id = '00000000-0000-0000-0000-00000000000d';
DELETE FROM iam_role_permissions WHERE role_id = '00000000-0000-0000-0000-000000000002';
DELETE FROM iam_users WHERE id = '00000000-0000-0000-0000-000000000002';
DELETE FROM iam_roles WHERE id = '00000000-0000-0000-0000-000000000002';
