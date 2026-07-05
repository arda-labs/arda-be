-- +goose Up
ALTER TABLE customers ADD COLUMN IF NOT EXISTS org_id VARCHAR(64) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS customer_amendments (
    id VARCHAR(64) PRIMARY KEY,
    customer_id VARCHAR(64) NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    workflow_case_id VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    before_snapshot JSONB NOT NULL DEFAULT '{}',
    after_snapshot JSONB NOT NULL DEFAULT '{}',
    changed_fields TEXT[] NOT NULL DEFAULT '{}',
    applied_at TIMESTAMPTZ,
    applied_by VARCHAR(100),
    rejected_at TIMESTAMPTZ,
    rejected_by VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_amendments_pending
    ON customer_amendments (customer_id)
    WHERE status IN ('DRAFT', 'PENDING');

CREATE INDEX IF NOT EXISTS idx_customer_amendments_customer
    ON customer_amendments (customer_id, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_customer_amendments_customer;
DROP INDEX IF EXISTS uq_customer_amendments_pending;
DROP TABLE IF EXISTS customer_amendments;
ALTER TABLE customers DROP COLUMN IF EXISTS org_id;
