-- +goose Up
-- Media permissions were created by 20260825100000_add_domain_gateway_permissions
-- but never assigned to business roles, so flow screens (Hồ sơ đính kèm tab)
-- would 403 on /api/media/** for every non-superadmin user. Media rows are
-- already tenant/org scoped in media-service, so attach/read is safe for all
-- internal roles; tighten to specific roles later if document types need it.
INSERT INTO iam_role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM iam_roles r
JOIN iam_permissions p ON p.code IN ('media.read', 'media.manage')
WHERE r.status = 'ACTIVE'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM iam_role_permissions
WHERE permission_id IN (
    SELECT id FROM iam_permissions WHERE code IN ('media.read', 'media.manage')
);
