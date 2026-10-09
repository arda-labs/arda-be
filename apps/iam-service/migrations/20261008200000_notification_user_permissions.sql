-- Notification users can mark their own inbox items read and manage their own
-- delivery preferences without receiving notification administration rights.
-- +goose Up
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'notification.read-mark', 'Mark own notifications read', 'notification', 'inbox', 'read-mark'),
    (uuidv7(), 'notification.preferences.manage', 'Manage own notification preferences', 'notification', 'preferences', 'manage')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions
WHERE code IN ('notification.read-mark', 'notification.preferences.manage');
