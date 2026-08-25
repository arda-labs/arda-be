-- +goose Up
-- Knowledge retrieval is intentionally separate from starting the assistant.
-- Assignment to business roles remains an explicit IAM administration decision.
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES (uuidv7(), 'ai.knowledge.read', 'Read approved Arda AI knowledge', 'ai', 'knowledge', 'read')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions WHERE code = 'ai.knowledge.read';
