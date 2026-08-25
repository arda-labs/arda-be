-- +goose Up
-- The permission is registered centrally; assignment to business roles remains
-- an explicit IAM administration decision.
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES (uuidv7(), 'ai.assistant.use', 'Use Arda AI assistant', 'ai', 'assistant', 'use')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions WHERE code = 'ai.assistant.use';
