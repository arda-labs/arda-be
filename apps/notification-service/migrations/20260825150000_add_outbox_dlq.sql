-- +goose Up
CREATE TABLE notification_outbox_dlq (
    outbox_id UUID PRIMARY KEY REFERENCES noti_outbox(id) ON DELETE RESTRICT,
    tenant_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    payload JSONB NOT NULL,
    attempts INTEGER NOT NULL,
    last_error TEXT NOT NULL,
    dead_lettered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    replayed_at TIMESTAMPTZ,
    replayed_by TEXT
);

CREATE INDEX notification_outbox_dlq_pending_idx
    ON notification_outbox_dlq (dead_lettered_at)
    WHERE replayed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS notification_outbox_dlq;
