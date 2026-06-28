-- +goose Up
ALTER TABLE iam_users ADD COLUMN IF NOT EXISTS nickname VARCHAR(255) DEFAULT '';

-- +goose Down
ALTER TABLE iam_users DROP COLUMN IF EXISTS nickname;
