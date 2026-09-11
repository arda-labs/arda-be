-- +goose Up

-- Notification templates + mail sender configs (X2/W6b).

CREATE TABLE IF NOT EXISTS noti_templates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  TEXT NOT NULL,
    event_code TEXT NOT NULL,
    channel    TEXT NOT NULL DEFAULT 'email',
    locale     TEXT NOT NULL DEFAULT 'vi-VN',
    subject    TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, event_code, channel, locale)
);

CREATE TABLE IF NOT EXISTS noti_sender_configs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL,
    channel      TEXT NOT NULL DEFAULT 'email',
    host         TEXT NOT NULL,
    port         INTEGER NOT NULL DEFAULT 587,
    username     TEXT NOT NULL DEFAULT '',
    password_enc TEXT NOT NULL DEFAULT '',
    from_address TEXT NOT NULL,
    from_name    TEXT NOT NULL DEFAULT '',
    use_tls      BOOLEAN NOT NULL DEFAULT true,
    is_active    BOOLEAN NOT NULL DEFAULT true,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, channel)
);

-- +goose Down

DROP TABLE IF EXISTS noti_sender_configs;
DROP TABLE IF EXISTS noti_templates;
