-- +goose Up
CREATE TABLE IF NOT EXISTS iam_identity_mappings (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    provider_id VARCHAR(100) NOT NULL,
    external_id VARCHAR(512) NOT NULL,
    internal_user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(provider_id, external_id)
);

CREATE INDEX idx_identity_mappings_provider ON iam_identity_mappings(provider_id, external_id);
CREATE INDEX idx_identity_mappings_user ON iam_identity_mappings(internal_user_id);

-- +goose Down
DROP TABLE IF EXISTS iam_identity_mappings CASCADE;
