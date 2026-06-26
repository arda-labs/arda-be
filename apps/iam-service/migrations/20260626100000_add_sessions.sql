-- +goose Up

CREATE TABLE IF NOT EXISTS iam_devices (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    device_name TEXT NOT NULL DEFAULT '',
    device_type TEXT NOT NULL DEFAULT 'browser',   -- browser, mobile_app, api_key
    os TEXT NOT NULL DEFAULT '',
    browser TEXT NOT NULL DEFAULT '',
    fingerprint_hash TEXT NOT NULL DEFAULT '',
    is_trusted BOOLEAN NOT NULL DEFAULT false,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_devices_user ON iam_devices(user_id);
CREATE INDEX idx_devices_fingerprint ON iam_devices(fingerprint_hash);

CREATE TABLE IF NOT EXISTS iam_sessions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    device_id UUID REFERENCES iam_devices(id) ON DELETE SET NULL,
    hydra_session_id TEXT,
    access_token_jti TEXT,
    refresh_token_jti TEXT,
    ip_address INET NOT NULL DEFAULT '0.0.0.0',
    user_agent TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_sessions_user_active ON iam_sessions(user_id, is_active) WHERE is_active = true;
CREATE INDEX idx_sessions_expires ON iam_sessions(expires_at) WHERE is_active = true;

CREATE TABLE IF NOT EXISTS iam_session_blacklist (
    jti TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_blacklist_expires ON iam_session_blacklist(expires_at);

-- +goose Down
DROP TABLE IF EXISTS iam_session_blacklist CASCADE;
DROP TABLE IF EXISTS iam_sessions CASCADE;
DROP TABLE IF EXISTS iam_devices CASCADE;
