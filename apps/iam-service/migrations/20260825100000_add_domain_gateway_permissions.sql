-- +goose Up
-- These permissions make auth-gateway route coverage explicit. Assignment to
-- business roles is intentionally domain-owned and happens in later waves.
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'finance.read', 'Read finance data', 'finance', '*', 'read'),
    (uuidv7(), 'finance.manage', 'Manage finance data', 'finance', '*', 'manage'),
    (uuidv7(), 'finance.approve', 'Approve finance operations', 'finance', 'approval', 'approve'),
    (uuidv7(), 'workflow.read', 'Read workflow data', 'workflow', '*', 'read'),
    (uuidv7(), 'workflow.manage', 'Manage workflow data', 'workflow', '*', 'manage'),
    (uuidv7(), 'workflow.operate', 'Operate workflow runtime', 'workflow', 'runtime', 'operate'),
    (uuidv7(), 'media.read', 'Read media files', 'media', 'file', 'read'),
    (uuidv7(), 'media.manage', 'Manage media files', 'media', 'file', 'manage'),
    (uuidv7(), 'notification.read', 'Read notifications', 'notification', '*', 'read'),
    (uuidv7(), 'notification.manage', 'Manage notifications', 'notification', '*', 'manage')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions
WHERE code IN (
    'finance.read', 'finance.manage', 'finance.approve',
    'workflow.read', 'workflow.manage', 'workflow.operate',
    'media.read', 'media.manage',
    'notification.read', 'notification.manage'
);
