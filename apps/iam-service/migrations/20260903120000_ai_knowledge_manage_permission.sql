-- +goose Up
-- Knowledge management (admin CRUD over sources/versions) is separate from
-- retrieval. Registered centrally; assignment to business roles remains an
-- explicit IAM administration decision. Matches policy.yaml rag-sources-*
-- and apps/rag-service/app/service/sources.py re-check.
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES (uuidv7(), 'ai.knowledge.manage', 'Manage Arda AI knowledge sources and versions', 'ai', 'knowledge', 'manage')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions WHERE code = 'ai.knowledge.manage';
