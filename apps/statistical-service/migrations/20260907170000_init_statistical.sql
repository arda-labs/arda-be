-- +goose Up

-- P2.3: statistical-service schema (QCMS, Q8): report definitions +
-- indicator catalogs + submission transactions. No free-form SQL: queries
-- are Go builders referenced by query_id.

CREATE TABLE IF NOT EXISTS rpt_report_definitions (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    code          VARCHAR(64) NOT NULL,
    name          VARCHAR(255) NOT NULL,
    group_code    VARCHAR(64),
    query_id      VARCHAR(64) NOT NULL,   -- Go query builder id (no SQL in DB)
    param_schema  JSONB NOT NULL DEFAULT '{}',
    template_file_id UUID,               -- media-service Excel template
    output_format VARCHAR(16) NOT NULL DEFAULT 'XLSX',
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_by    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS rpt_indicators (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   VARCHAR(64) NOT NULL,
    code        VARCHAR(64) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    unit        VARCHAR(32),
    group_code  VARCHAR(64),
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS rpt_report_submissions (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    report_code   VARCHAR(64) NOT NULL,
    period_code   VARCHAR(7) NOT NULL,     -- "2026-09"
    status        VARCHAR(16) NOT NULL DEFAULT 'DRAFT', -- DRAFT|SUBMITTED|APPROVED|REJECTED
    payload       JSONB NOT NULL DEFAULT '{}',
    workflow_case_id UUID,
    submitted_by  TEXT,
    submitted_at  TIMESTAMPTZ,
    created_by    TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, report_code, period_code)
);

-- +goose Down
SELECT 1;
