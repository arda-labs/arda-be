-- +goose Up
ALTER TABLE media_files ADD COLUMN expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_media_files_temp_expires
  ON media_files (status, expires_at)
  WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_media_files_temp_expires;
ALTER TABLE media_files DROP COLUMN IF EXISTS expires_at;
