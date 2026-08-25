-- +goose Up
-- Proposal and approval capabilities remain separate from starting the AI
-- assistant. Role assignment is an explicit IAM administration decision.
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'ai.approval.propose', 'Propose an Arda AI action', 'ai', 'approval', 'propose'),
    (uuidv7(), 'ai.approval.execute', 'Approve an Arda AI action', 'ai', 'approval', 'execute')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions
WHERE code IN ('ai.approval.propose', 'ai.approval.execute');
