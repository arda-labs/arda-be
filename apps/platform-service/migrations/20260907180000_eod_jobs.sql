-- +goose Up

-- P2.4: EOD COB engine (service-boundary-design §6): job definitions +
-- runs with checkpoint + business-date idempotency.

CREATE TABLE IF NOT EXISTS plt_job_definitions (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   VARCHAR(64) NOT NULL,
    code        VARCHAR(64) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    sequence    INTEGER NOT NULL DEFAULT 0,
    endpoint    TEXT NOT NULL,             -- internal job URL (e.g. loan accrual-daily)
    payload     JSONB NOT NULL DEFAULT '{}',
    is_enabled  BOOLEAN NOT NULL DEFAULT true,
    created_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS plt_job_runs (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    VARCHAR(64) NOT NULL,
    job_code     VARCHAR(64) NOT NULL,
    business_date DATE NOT NULL,
    status       VARCHAR(16) NOT NULL DEFAULT 'RUNNING', -- RUNNING|DONE|FAILED
    checkpoint   JSONB NOT NULL DEFAULT '{}',
    error        TEXT,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ,
    UNIQUE (tenant_id, job_code, business_date)
);

CREATE INDEX IF NOT EXISTS idx_plt_job_runs_tenant ON plt_job_runs (tenant_id, business_date);

-- +goose Down
SELECT 1;
