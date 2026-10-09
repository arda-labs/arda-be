-- +goose Up
ALTER TABLE noti_inbox
    ADD COLUMN entity_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN entity_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN dedupe_key TEXT,
    ADD COLUMN resolved_at TIMESTAMPTZ,
    ADD COLUMN resolved_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN superseded_at TIMESTAMPTZ,
    ADD COLUMN expires_at TIMESTAMPTZ,
    ADD COLUMN locale TEXT NOT NULL DEFAULT '',
    ADD COLUMN priority INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN event_seq BIGINT;

UPDATE noti_inbox
SET dedupe_key = 'legacy:' || public_id,
    event_seq = row_number
FROM (
    SELECT id, row_number() OVER (
        PARTITION BY tenant_id, user_id ORDER BY created_at, id
    ) AS row_number
    FROM noti_inbox
) legacy
WHERE noti_inbox.id = legacy.id;

ALTER TABLE noti_inbox
    ALTER COLUMN dedupe_key SET NOT NULL,
    ALTER COLUMN event_seq SET NOT NULL;

CREATE UNIQUE INDEX noti_inbox_tenant_user_dedupe_uq
    ON noti_inbox (tenant_id, user_id, dedupe_key);
CREATE INDEX noti_inbox_entity_unresolved_idx
    ON noti_inbox (tenant_id, entity_type, entity_id)
    WHERE resolved_at IS NULL AND entity_type <> '' AND entity_id <> '';
CREATE INDEX noti_inbox_user_event_seq_idx
    ON noti_inbox (tenant_id, user_id, event_seq DESC);

CREATE TABLE noti_user_seq (
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    last_event_seq BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, user_id)
);

INSERT INTO noti_user_seq (tenant_id, user_id, last_event_seq)
SELECT tenant_id, user_id, max(event_seq)
FROM noti_inbox
GROUP BY tenant_id, user_id
ON CONFLICT (tenant_id, user_id) DO UPDATE
SET last_event_seq = GREATEST(noti_user_seq.last_event_seq, EXCLUDED.last_event_seq);

CREATE TABLE noti_preferences (
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    event_group TEXT NOT NULL,
    channel TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    quiet_start TIME,
    quiet_end TIME,
    tz TEXT NOT NULL DEFAULT 'UTC',
    digest_mode TEXT NOT NULL DEFAULT 'NONE',
    locale TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id, event_group, channel),
    CHECK (digest_mode IN ('NONE', 'HOURLY', 'DAILY')),
    CHECK ((quiet_start IS NULL) = (quiet_end IS NULL))
);
CREATE INDEX noti_preferences_user_idx
    ON noti_preferences (tenant_id, user_id, event_group);

-- Resolutions are durable tombstones so a completion event that arrives before
-- its assignment event cannot recreate an actionable notification later.
CREATE TABLE noti_entity_resolutions (
    tenant_id TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    resolved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_reason TEXT NOT NULL DEFAULT '',
    source_event_id TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, entity_type, entity_id)
);

CREATE TABLE noti_recipient_warnings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, event_id)
);
CREATE INDEX noti_recipient_warnings_recent_idx
    ON noti_recipient_warnings (tenant_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS noti_recipient_warnings;
DROP TABLE IF EXISTS noti_entity_resolutions;
DROP TABLE IF EXISTS noti_preferences;
DROP TABLE IF EXISTS noti_user_seq;
DROP INDEX IF EXISTS noti_inbox_user_event_seq_idx;
DROP INDEX IF EXISTS noti_inbox_entity_unresolved_idx;
DROP INDEX IF EXISTS noti_inbox_tenant_user_dedupe_uq;
ALTER TABLE noti_inbox
    DROP COLUMN IF EXISTS event_seq,
    DROP COLUMN IF EXISTS priority,
    DROP COLUMN IF EXISTS locale,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS resolved_reason,
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS superseded_at,
    DROP COLUMN IF EXISTS dedupe_key,
    DROP COLUMN IF EXISTS entity_id,
    DROP COLUMN IF EXISTS entity_type;
