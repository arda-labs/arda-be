-- +goose Up

-- Proactive reporting (step 7): rules over computed indicators and the alerts
-- they raise. Instead of waiting for someone to open a report, the EOD loop
-- evaluates each rule against the period's stored result and records an alert
-- when it is breached.
--
-- Rules compare a STORED indicator value to a threshold; they never carry SQL.
-- The indicator code must exist and be computable, so this stays inside the
-- same closed surface as the indicator engine (a rule can only watch something
-- the engine can already produce).

CREATE TABLE IF NOT EXISTS rpt_indicator_rules (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL,
    code           VARCHAR(64) NOT NULL,
    name           VARCHAR(255) NOT NULL,
    indicator_code VARCHAR(64) NOT NULL,
    -- Empty = the total series; a DimensionKey selects one slice.
    dimension_key  VARCHAR(255) NOT NULL DEFAULT '',
    operator       VARCHAR(4) NOT NULL,
    threshold      NUMERIC(20,6) NOT NULL,
    -- INFO | WARN | CRITICAL
    severity       VARCHAR(16) NOT NULL DEFAULT 'WARN',
    message        TEXT NOT NULL DEFAULT '',
    is_active      BOOLEAN NOT NULL DEFAULT true,
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code),
    CONSTRAINT rpt_indicator_rules_operator_ck
        CHECK (operator IN ('>', '>=', '<', '<=', '=', '<>')),
    CONSTRAINT rpt_indicator_rules_severity_ck
        CHECK (severity IN ('INFO', 'WARN', 'CRITICAL'))
);

CREATE INDEX IF NOT EXISTS idx_rpt_indicator_rules_active
    ON rpt_indicator_rules (tenant_id, is_active);

CREATE TABLE IF NOT EXISTS rpt_indicator_alerts (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id       VARCHAR(64) NOT NULL,
    rule_code       VARCHAR(64) NOT NULL,
    indicator_code  VARCHAR(64) NOT NULL,
    dimension_key   VARCHAR(255) NOT NULL DEFAULT '',
    period_code     VARCHAR(16) NOT NULL,
    value           NUMERIC(20,6),
    threshold       NUMERIC(20,6) NOT NULL,
    operator        VARCHAR(4) NOT NULL,
    severity        VARCHAR(16) NOT NULL,
    message         TEXT NOT NULL DEFAULT '',
    -- OPEN | ACKED
    status          VARCHAR(16) NOT NULL DEFAULT 'OPEN',
    acknowledged_by TEXT NOT NULL DEFAULT '',
    acknowledged_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- One alert per rule + period + slice: re-evaluating a period updates the
    -- same row instead of spamming a new alert every COB.
    UNIQUE (tenant_id, rule_code, period_code, dimension_key)
);

CREATE INDEX IF NOT EXISTS idx_rpt_indicator_alerts_open
    ON rpt_indicator_alerts (tenant_id, status, period_code);

-- +goose Down
DROP INDEX IF EXISTS idx_rpt_indicator_alerts_open;
DROP TABLE IF EXISTS rpt_indicator_alerts;
DROP INDEX IF EXISTS idx_rpt_indicator_rules_active;
DROP TABLE IF EXISTS rpt_indicator_rules;
