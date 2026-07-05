-- +goose Up
INSERT INTO iam_user_roles (user_id, role_id)
SELECT u.id, r.id
FROM iam_users u
JOIN iam_roles r ON r.code IN ('CRM_CUSTOMER_MAKER', 'CRM_CUSTOMER_CHECKER')
WHERE u.username IN ('admin', 'superadmin')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_user_roles ur
USING iam_users u, iam_roles r
WHERE ur.user_id = u.id
  AND ur.role_id = r.id
  AND u.username IN ('admin', 'superadmin')
  AND r.code IN ('CRM_CUSTOMER_MAKER', 'CRM_CUSTOMER_CHECKER');
