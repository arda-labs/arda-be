-- +goose Up
ALTER TABLE iam_users ADD COLUMN IF NOT EXISTS avatar_file_id TEXT;
ALTER TABLE iam_users ADD COLUMN IF NOT EXISTS picture_url TEXT;

-- +goose Down
ALTER TABLE iam_users DROP COLUMN IF EXISTS picture_url;
ALTER TABLE iam_users DROP COLUMN IF EXISTS avatar_file_id;
