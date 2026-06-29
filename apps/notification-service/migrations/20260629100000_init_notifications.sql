-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE noti_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_id TEXT NOT NULL UNIQUE,
    tenant_id TEXT NOT NULL,
    source_service TEXT NOT NULL DEFAULT '',
    source_event_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL DEFAULT '',
    recipients JSONB NOT NULL DEFAULT '[]'::jsonb,
    channels JSONB NOT NULL DEFAULT '[]'::jsonb,
    template_key TEXT NOT NULL,
    template_version INTEGER NOT NULL DEFAULT 0,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'accepted',
    idempotency_key TEXT NOT NULL,
    correlation_id TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX idx_noti_notifications_tenant_created ON noti_notifications (tenant_id, created_at DESC);
CREATE INDEX idx_noti_notifications_status_created ON noti_notifications (status, created_at DESC);

CREATE TABLE noti_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID NOT NULL REFERENCES noti_notifications(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    channel TEXT NOT NULL,
    destination JSONB NOT NULL DEFAULT '{}'::jsonb,
    provider TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 6,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error_message TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_noti_deliveries_claim
    ON noti_deliveries (status, next_attempt_at, created_at)
    WHERE status IN ('queued', 'retrying');
CREATE INDEX idx_noti_deliveries_notification ON noti_deliveries (notification_id);

CREATE TABLE noti_inbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_id TEXT NOT NULL UNIQUE,
    notification_id UUID REFERENCES noti_notifications(id) ON DELETE SET NULL,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'info',
    title_key TEXT NOT NULL DEFAULT '',
    body_key TEXT NOT NULL DEFAULT '',
    params JSONB NOT NULL DEFAULT '{}'::jsonb,
    href TEXT NOT NULL DEFAULT '',
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_noti_inbox_user_created ON noti_inbox (tenant_id, user_id, created_at DESC);
CREATE INDEX idx_noti_inbox_user_unread ON noti_inbox (tenant_id, user_id, created_at DESC) WHERE read_at IS NULL;

CREATE TABLE noti_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject TEXT NOT NULL,
    event_code TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX idx_noti_outbox_pending ON noti_outbox (status, next_retry_at, locked_until, created_at);

CREATE TABLE noti_delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id UUID NOT NULL REFERENCES noti_deliveries(id) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL,
    status TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    request_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX idx_noti_delivery_attempts_delivery ON noti_delivery_attempts (delivery_id, attempt_no);

-- +goose Down
DROP TABLE IF EXISTS noti_delivery_attempts;
DROP TABLE IF EXISTS noti_outbox;
DROP TABLE IF EXISTS noti_inbox;
DROP TABLE IF EXISTS noti_deliveries;
DROP TABLE IF EXISTS noti_notifications;
