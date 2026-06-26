-- +goose Up

-- Add tamper-proof fields to audit logs
ALTER TABLE iam_audit_logs
    ADD COLUMN IF NOT EXISTS event_id TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS chain_prev_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS chain_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_audit_tenant ON iam_audit_logs(tenant_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_result ON iam_audit_logs(result, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_id ON iam_audit_logs(event_id);

-- +goose Down
ALTER TABLE iam_audit_logs
    DROP COLUMN IF EXISTS event_id,
    DROP COLUMN IF EXISTS tenant_id,
    DROP COLUMN IF EXISTS chain_prev_hash,
    DROP COLUMN IF EXISTS chain_hash;
