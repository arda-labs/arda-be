-- +goose Up
CREATE TABLE IF NOT EXISTS noti_push_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (endpoint)
);

CREATE INDEX IF NOT EXISTS idx_noti_push_subscriptions_user
    ON noti_push_subscriptions (tenant_id, user_id);

-- +goose Down
DROP TABLE IF EXISTS noti_push_subscriptions;
