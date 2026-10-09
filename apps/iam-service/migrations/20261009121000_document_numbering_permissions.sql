-- +goose Up
INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
    (uuidv7(), 'doc.renumber', 'Request document renumbering', 'finance', 'document-number', 'renumber'),
    (uuidv7(), 'doc.renumber.locked', 'Approve document renumbering in closed periods', 'finance', 'document-number', 'renumber-locked')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM iam_permissions WHERE code IN ('doc.renumber', 'doc.renumber.locked');
