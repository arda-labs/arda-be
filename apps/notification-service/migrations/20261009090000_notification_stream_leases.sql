-- +goose Up
CREATE TABLE noti_stream_lease (
    lease_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX noti_stream_lease_user_expiry_idx
    ON noti_stream_lease (tenant_id, user_id, expires_at);

-- +goose Down
DROP TABLE IF EXISTS noti_stream_lease;
