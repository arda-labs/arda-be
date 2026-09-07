-- +goose Up
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'crm.project.read', 'Read CRM projects', 'crm', 'project', 'read'),
    (uuidv7(), 'crm.project.manage', 'Manage CRM projects', 'crm', 'project', 'manage')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN ('crm.project.read', 'crm.project.manage')
WHERE r.code = 'CRM_CUSTOMER_MAKER'
ON CONFLICT DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code = 'crm.project.read'
WHERE r.code = 'CRM_CUSTOMER_CHECKER'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_role_permissions
WHERE permission_id IN (SELECT id FROM iam_permissions WHERE code IN ('crm.project.read', 'crm.project.manage'));

DELETE FROM iam_permissions WHERE code IN ('crm.project.read', 'crm.project.manage');
