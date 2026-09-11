-- +goose Up

-- QCMS import transaction staging (fe_statistical #20): staged import rows
-- per (tenant, import type, period). import_type_code references the
-- `import-type` catalog kind; POSTED submissions reuse RPT_SUBMIT_V2.

CREATE TABLE IF NOT EXISTS rpt_import_transactions (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    import_type_code VARCHAR(64) NOT NULL,
    period_code      VARCHAR(7) NOT NULL,
    status           VARCHAR(16) NOT NULL DEFAULT 'STAGED', -- STAGED|VALIDATED|POSTED|REJECTED
    row_count        INTEGER NOT NULL DEFAULT 0,
    payload          JSONB NOT NULL DEFAULT '{}',
    workflow_case_id UUID,
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, import_type_code, period_code)
);

CREATE INDEX IF NOT EXISTS idx_rpt_import_txn_tenant
    ON rpt_import_transactions (tenant_id, period_code, status);

-- +goose Down

DROP TABLE IF EXISTS rpt_import_transactions;
