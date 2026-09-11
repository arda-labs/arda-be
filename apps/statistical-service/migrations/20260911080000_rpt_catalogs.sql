-- +goose Up

-- QCMS generic catalogs (W5): one table for the ~12 EPAS stat/QCMS catalog
-- screens (indicator type, stat code map, regulation, response definition,
-- txn status, rule definition/type, KPI type, report group/param, import +
-- CMMS scenario groups). attributes jsonb carries kind-specific fields.

CREATE TABLE IF NOT EXISTS rpt_catalog_items (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  VARCHAR(64) NOT NULL,
    kind       VARCHAR(32) NOT NULL,
    code       VARCHAR(64) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    parent_code VARCHAR(64) NOT NULL DEFAULT '',
    attributes JSONB NOT NULL DEFAULT '{}',
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, kind, code)
);

CREATE INDEX IF NOT EXISTS idx_rpt_catalog_kind ON rpt_catalog_items (tenant_id, kind);

-- Seed a few reference rows for the pilot tenant so the FE tabs are not empty.
INSERT INTO rpt_catalog_items (tenant_id, kind, code, name, parent_code, attributes, created_by)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'indicator-type', 'BALANCE', 'Chỉ tiêu số dư', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'indicator-type', 'FLOW', 'Chỉ tiêu phát sinh', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'txn-status', 'DRAFT', 'Nháp', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'txn-status', 'SUBMITTED', 'Đã nộp', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'txn-status', 'APPROVED', 'Đã duyệt', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'txn-status', 'REJECTED', 'Từ chối', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'report-group', 'LNM', 'Tín dụng', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'report-group', 'DPM', 'Tiền gửi', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'report-group', 'CFM', 'Nguồn vốn', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'kpi-type', 'RATIO', 'Tỷ lệ', '', '{}', 'seed'),
    ('00000000-0000-0000-0000-000000000010', 'kpi-type', 'AMOUNT', 'Giá trị', '', '{}', 'seed')
ON CONFLICT (tenant_id, kind, code) DO NOTHING;

-- +goose Down

DROP TABLE IF EXISTS rpt_catalog_items;
