-- +goose Up

CREATE TABLE workflow_outbox (
    id VARCHAR(64) PRIMARY KEY,
    subject VARCHAR(160) NOT NULL,
    event_code VARCHAR(120) NOT NULL,
    dedupe_key VARCHAR(255) NOT NULL UNIQUE,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT
);

CREATE INDEX workflow_outbox_pending_idx ON workflow_outbox (created_at, id)
    WHERE published_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS workflow_outbox;
