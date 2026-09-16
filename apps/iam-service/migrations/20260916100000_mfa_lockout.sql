-- +goose Up

ALTER TABLE iam_mfa_settings
    ADD COLUMN IF NOT EXISTS failed_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;

-- +goose Down

ALTER TABLE iam_mfa_settings
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS failed_attempts;
