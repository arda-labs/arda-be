-- Permission management is split from user management at the gateway. The
-- delete capability is intentionally explicit; iam.permission.create must not
-- authorize destructive deletion.

-- +goose Up
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES (uuidv7(), 'iam.permission.delete', 'Delete IAM permissions', 'iam', 'permission', 'delete')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions WHERE code = 'iam.permission.delete';
