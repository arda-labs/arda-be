-- +goose Up
-- Generic consumer inbox for at-least-once JetStream delivery. Business
-- handlers must use ProcessEventOnce so their local mutation and the processed
-- marker commit in one database transaction before acknowledging NATS.
CREATE TABLE noti_event_inbox (
    consumer_name TEXT NOT NULL,
    event_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 1,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (consumer_name, event_id)
);

CREATE INDEX noti_event_inbox_pending_idx
    ON noti_event_inbox (consumer_name, received_at)
    WHERE processed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS noti_event_inbox;
