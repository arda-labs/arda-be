-- +goose Up

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'platform.read', 'Read Platform Data', 'platform', '*', 'read'),
    (uuidv7(), 'platform.manage', 'Manage Platform Data', 'platform', '*', 'manage')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN ('platform.read', 'platform.manage')
WHERE r.code = 'SUPER_ADMIN'
ON CONFLICT DO NOTHING;

INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3)
SELECT 'p', 'ADMIN', 'platform:*', '*', 'allow'
WHERE NOT EXISTS (
    SELECT 1 FROM iam_casbin_rules
    WHERE ptype = 'p' AND v0 = 'ADMIN' AND v1 = 'platform:*' AND v2 = '*' AND v3 = 'allow'
);

-- +goose Down
DELETE FROM iam_casbin_rules WHERE ptype = 'p' AND v0 = 'ADMIN' AND v1 = 'platform:*' AND v2 = '*' AND v3 = 'allow';
DELETE FROM iam_role_permissions
WHERE permission_id IN (SELECT id FROM iam_permissions WHERE code IN ('platform.read', 'platform.manage'));
DELETE FROM iam_permissions WHERE code IN ('platform.read', 'platform.manage');

