-- +goose Up

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'loan.read', 'Read Loan Data', 'loan', '*', 'read'),
    (uuidv7(), 'loan.manage', 'Manage Loan Data', 'loan', '*', 'manage')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN ('loan.read', 'loan.manage')
WHERE r.code = 'SUPER_ADMIN'
ON CONFLICT DO NOTHING;

INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3)
SELECT 'p', 'ADMIN', 'loan:*', '*', 'allow'
WHERE NOT EXISTS (
    SELECT 1 FROM iam_casbin_rules
    WHERE ptype = 'p' AND v0 = 'ADMIN' AND v1 = 'loan:*' AND v2 = '*' AND v3 = 'allow'
);

-- +goose Down
DELETE FROM iam_casbin_rules WHERE ptype = 'p' AND v0 = 'ADMIN' AND v1 = 'loan:*' AND v2 = '*' AND v3 = 'allow';
DELETE FROM iam_role_permissions
WHERE permission_id IN (SELECT id FROM iam_permissions WHERE code IN ('loan.read', 'loan.manage'));
DELETE FROM iam_permissions WHERE code IN ('loan.read', 'loan.manage');
