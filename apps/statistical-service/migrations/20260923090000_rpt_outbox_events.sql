-- +goose Up

-- Outbox for statistical-service events (currently the indicator threshold
-- alerts). The alert row is the durable record; this table is the delivery
-- queue a relay drains to NATS JetStream, where notification-service picks the
-- subject up and renders it through noti_templates.
--
-- dedupe_key makes enqueue idempotent across COB re-runs and backfills: one
-- event per (rule, period, slice, value). A breach that keeps the same value
-- notifies once; a materially different value notifies again.

CREATE TABLE IF NOT EXISTS rpt_outbox_events (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    subject          VARCHAR(128) NOT NULL,
    aggregate_type   VARCHAR(64) NOT NULL DEFAULT '',
    aggregate_id     VARCHAR(128) NOT NULL DEFAULT '',
    dedupe_key       VARCHAR(255) NOT NULL,
    payload          JSONB NOT NULL,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT NOT NULL DEFAULT '',
    published_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_rpt_outbox_pending
    ON rpt_outbox_events (created_at)
    WHERE published_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_rpt_outbox_pending;
DROP TABLE IF EXISTS rpt_outbox_events;
