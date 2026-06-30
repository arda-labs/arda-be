-- +goose Up
ALTER TABLE iam_users
  ADD COLUMN IF NOT EXISTS auth_version BIGINT NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE iam_users
  DROP COLUMN IF EXISTS auth_version;
