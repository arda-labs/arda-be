-- +goose Up
-- OCR of identity documents. Granted to every role that can already manage
-- customers (they register customers from a CCCD); other modules get the
-- permission by explicit IAM administration.
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES (uuidv7(), 'document.extract', 'Extract data from identity documents', 'document', 'extract', 'use')
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT rp.role_id, p.id
FROM iam_role_permissions rp
JOIN iam_permissions c ON c.id = rp.permission_id AND c.code = 'crm.customer.manage'
CROSS JOIN iam_permissions p
WHERE p.code = 'document.extract'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_role_permissions
WHERE permission_id IN (SELECT id FROM iam_permissions WHERE code = 'document.extract');
DELETE FROM iam_permissions WHERE code = 'document.extract';
