-- +goose Up

ALTER TABLE iam_devices
    ADD COLUMN IF NOT EXISTS device_token_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS trusted_until TIMESTAMPTZ;

UPDATE iam_devices
SET trusted_until = now() + interval '30 days'
WHERE is_trusted = true AND trusted_until IS NULL;

CREATE INDEX IF NOT EXISTS idx_devices_token ON iam_devices(user_id, device_token_hash);
CREATE INDEX IF NOT EXISTS idx_devices_trusted_until ON iam_devices(trusted_until);

-- +goose Down

DROP INDEX IF EXISTS idx_devices_trusted_until;
DROP INDEX IF EXISTS idx_devices_token;

ALTER TABLE iam_devices
    DROP COLUMN IF EXISTS trusted_until,
    DROP COLUMN IF EXISTS device_token_hash;
