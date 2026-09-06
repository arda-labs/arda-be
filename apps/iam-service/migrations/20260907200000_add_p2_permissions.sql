-- +goose Up

-- P2 permissions for deposit/capital/statistical domains.

INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
VALUES
  (uuidv7(), 'deposit.read',     'Read deposit data',     'deposit',     '*', 'read'),
  (uuidv7(), 'deposit.manage',   'Manage deposit data',   'deposit',     '*', 'manage'),
  (uuidv7(), 'capital.read',     'Read capital data',     'capital',     '*', 'read'),
  (uuidv7(), 'capital.manage',   'Manage capital data',   'capital',     '*', 'manage'),
  (uuidv7(), 'statistical.read', 'Read statistical data', 'statistical', '*', 'read'),
  (uuidv7(), 'statistical.manage','Manage statistical data','statistical','*', 'manage')
ON CONFLICT DO NOTHING;

-- +goose Down
SELECT 1;
