-- +goose Up

-- CMMS compliance run results (fe_statistical #21): one row per scenario +
-- compliance period. Scenario/period config lives in the QCMS catalogs
-- (`cmms-scenario`, `cmms-scenario-group`, `cmms-compliance-period`).

CREATE TABLE IF NOT EXISTS rpt_cmms_results (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         VARCHAR(64) NOT NULL,
    scenario_code     VARCHAR(64) NOT NULL,
    compliance_period VARCHAR(7) NOT NULL, -- "YYYY-MM"
    status            VARCHAR(16) NOT NULL DEFAULT 'PASSED', -- PASSED|FAILED
    checked_count     INTEGER NOT NULL DEFAULT 0,
    failed_count      INTEGER NOT NULL DEFAULT 0,
    details           JSONB NOT NULL DEFAULT '{}',
    run_by            TEXT NOT NULL DEFAULT '',
    run_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, scenario_code, compliance_period)
);

CREATE INDEX IF NOT EXISTS idx_rpt_cmms_results_tenant
    ON rpt_cmms_results (tenant_id, compliance_period, status);

-- +goose Down

DROP TABLE IF EXISTS rpt_cmms_results;
