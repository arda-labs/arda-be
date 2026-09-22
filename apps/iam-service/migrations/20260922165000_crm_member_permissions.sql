-- +goose Up

-- QTDND membership permissions and the maker/checker roles that own the member
-- book. Separate from CRM_CUSTOMER_* so member-duty owners are distinct from
-- customer-registration owners (the workflow registers CRM_MAKER/CRM_CHECKER
-- for CRM_MEMBER_V1; these IAM roles carry the API permissions).

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'crm.member.read',   'Read CRM members',   'crm', 'member', 'read'),
    (uuidv7(), 'crm.member.manage', 'Manage CRM members', 'crm', 'member', 'manage')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_roles (id, code, name, status, tenant_id)
VALUES
    (uuidv7(), 'CRM_MEMBER_MAKER',   'CRM member maker',   'ACTIVE', 'default'),
    (uuidv7(), 'CRM_MEMBER_CHECKER', 'CRM member checker', 'ACTIVE', 'default')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN ('crm.member.read', 'crm.member.manage')
WHERE r.code = 'CRM_MEMBER_MAKER'
ON CONFLICT DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code = 'crm.member.read'
WHERE r.code = 'CRM_MEMBER_CHECKER'
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM iam_role_permissions
WHERE role_id IN (SELECT id FROM iam_roles WHERE code IN ('CRM_MEMBER_MAKER', 'CRM_MEMBER_CHECKER'));

DELETE FROM iam_roles WHERE code IN ('CRM_MEMBER_MAKER', 'CRM_MEMBER_CHECKER');

DELETE FROM iam_permissions WHERE code IN ('crm.member.read', 'crm.member.manage');
