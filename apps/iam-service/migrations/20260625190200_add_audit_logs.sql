-- +goose Up
CREATE TABLE IF NOT EXISTS iam_audit_logs (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
    event_type VARCHAR(100) NOT NULL,
    subject VARCHAR(255),
    action VARCHAR(255),
    resource VARCHAR(255),
    result VARCHAR(50) NOT NULL,
    details JSONB,
    client_ip VARCHAR(45),
    user_agent TEXT,
    request_id VARCHAR(100),
    service_name VARCHAR(100) DEFAULT 'iam-service',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_timestamp ON iam_audit_logs(timestamp DESC);
CREATE INDEX idx_audit_logs_event_type ON iam_audit_logs(event_type);
CREATE INDEX idx_audit_logs_subject ON iam_audit_logs(subject);

-- +goose Down
DROP TABLE IF EXISTS iam_audit_logs CASCADE;
