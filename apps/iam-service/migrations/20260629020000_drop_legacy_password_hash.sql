-- +goose Up
ALTER TABLE iam_users DROP COLUMN IF EXISTS password_hash;

-- +goose Down
ALTER TABLE iam_users ADD COLUMN IF NOT EXISTS password_hash VARCHAR(512);
