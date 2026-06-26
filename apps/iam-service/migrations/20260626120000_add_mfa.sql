-- +goose Up

CREATE TABLE IF NOT EXISTS iam_mfa_settings (
    user_id UUID PRIMARY KEY REFERENCES iam_users(id) ON DELETE CASCADE,
    method TEXT NOT NULL DEFAULT 'totp',
    secret TEXT NOT NULL DEFAULT '',
    is_enrolled BOOLEAN NOT NULL DEFAULT false,
    enrolled_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS iam_mfa_backup_codes (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    is_used BOOLEAN NOT NULL DEFAULT false,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_mfa_backup_user ON iam_mfa_backup_codes(user_id);

-- +goose Down
DROP TABLE IF EXISTS iam_mfa_backup_codes CASCADE;
DROP TABLE IF EXISTS iam_mfa_settings CASCADE;
