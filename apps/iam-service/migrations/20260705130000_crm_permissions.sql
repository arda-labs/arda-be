-- +goose Up
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'crm.customer.read', 'Read CRM customers', 'crm', 'customer', 'read'),
    (uuidv7(), 'crm.customer.manage', 'Manage CRM customers', 'crm', 'customer', 'manage')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_roles (id, code, name, status, tenant_id)
VALUES
    (uuidv7(), 'CRM_CUSTOMER_MAKER', 'CRM customer maker', 'ACTIVE', 'default'),
    (uuidv7(), 'CRM_CUSTOMER_CHECKER', 'CRM customer checker', 'ACTIVE', 'default')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN ('crm.customer.read', 'crm.customer.manage')
WHERE r.code = 'CRM_CUSTOMER_MAKER'
ON CONFLICT DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code = 'crm.customer.read'
WHERE r.code = 'CRM_CUSTOMER_CHECKER'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_role_permissions
WHERE role_id IN (SELECT id FROM iam_roles WHERE code IN ('CRM_CUSTOMER_MAKER', 'CRM_CUSTOMER_CHECKER'));

DELETE FROM iam_roles WHERE code IN ('CRM_CUSTOMER_MAKER', 'CRM_CUSTOMER_CHECKER');

DELETE FROM iam_permissions WHERE code IN ('crm.customer.read', 'crm.customer.manage');
