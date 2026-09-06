-- +goose Up

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'mdm.read', 'Read Master Data', 'mdm', '*', 'read'),
    (uuidv7(), 'mdm.manage', 'Manage Master Data', 'mdm', '*', 'manage')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN ('mdm.read', 'mdm.manage')
WHERE r.code = 'SUPER_ADMIN'
ON CONFLICT DO NOTHING;

INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3)
SELECT 'p', 'ADMIN', 'mdm:*', '*', 'allow'
WHERE NOT EXISTS (
    SELECT 1 FROM iam_casbin_rules
    WHERE ptype = 'p' AND v0 = 'ADMIN' AND v1 = 'mdm:*' AND v2 = '*' AND v3 = 'allow'
);

-- +goose Down
DELETE FROM iam_casbin_rules WHERE ptype = 'p' AND v0 = 'ADMIN' AND v1 = 'mdm:*' AND v2 = '*' AND v3 = 'allow';
DELETE FROM iam_role_permissions
WHERE permission_id IN (SELECT id FROM iam_permissions WHERE code IN ('mdm.read', 'mdm.manage'));
DELETE FROM iam_permissions WHERE code IN ('mdm.read', 'mdm.manage');
