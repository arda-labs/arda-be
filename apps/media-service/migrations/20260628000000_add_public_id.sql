-- +goose Up
ALTER TABLE media_files ADD COLUMN public_id TEXT;
UPDATE media_files SET public_id = 'mf_' || SUBSTRING(id FROM 6) WHERE public_id IS NULL;
ALTER TABLE media_files ALTER COLUMN public_id SET NOT NULL;
ALTER TABLE media_files ADD CONSTRAINT media_files_public_id_key UNIQUE (public_id);
CREATE INDEX IF NOT EXISTS idx_media_files_public_id ON media_files(public_id) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_media_files_public_id;
ALTER TABLE media_files DROP CONSTRAINT IF EXISTS media_files_public_id_key;
ALTER TABLE media_files DROP COLUMN IF EXISTS public_id;
