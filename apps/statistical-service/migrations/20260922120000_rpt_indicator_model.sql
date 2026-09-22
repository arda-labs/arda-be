-- +goose Up

-- P3 reporting data layer, step 3 (arda-be/docs/reporting-data-layer.md): the
-- indicator catalog grows from code/name/unit into the QCMS KPI model the
-- 923-indicator PCF catalog needs (docs/epas-survey/exports/pcf-kpi-catalog.json),
-- plus the result store the computed values land in.
--
-- Deliberately NOT the EPAS shape: EPAS stored a 4000-char EXPRESSION_SQL per
-- KPI (SQL-as-config). Arda keeps query logic in Go and stores only declarative
-- references: kpi_type (P/C), periodicity, a JSON `formula` whose members are
-- {indicator|fact, column, sign}, source tables, and the analysis dimensions.
-- No SQL text is stored anywhere.

ALTER TABLE rpt_indicators
    ADD COLUMN IF NOT EXISTS kpi_type        VARCHAR(1)  NOT NULL DEFAULT 'P',   -- P (primary) | C (calculated)
    ADD COLUMN IF NOT EXISTS periodicity     VARCHAR(8)  NOT NULL DEFAULT 'D',   -- D|W|M|Q|Y
    ADD COLUMN IF NOT EXISTS is_ratio        BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS root_code       VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS root_name       VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS meaning         TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sources         JSONB       NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS formula         JSONB       NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS dimensions      JSONB       NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS display_format  VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS rounding_digits INTEGER     NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS score_group     VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS weight          NUMERIC(9,6);

ALTER TABLE rpt_indicators
    ADD CONSTRAINT rpt_indicators_kpi_type_check CHECK (kpi_type IN ('P', 'C'));

CREATE INDEX IF NOT EXISTS idx_rpt_indicators_topic
    ON rpt_indicators (tenant_id, group_code, kpi_type);

-- Indicator results: one value per (tenant, indicator, period, dimension key).
-- dimension_key is a deterministic string built from the report's dimensions
-- (e.g. "org=01|term=12"), so the same indicator can hold a total and its
-- breakdowns side by side. revision supports the maker-checker resubmission
-- model borrowed from EPAS RPT_TXN_STAT_TEMPLATE.
CREATE TABLE IF NOT EXISTS rpt_indicator_results (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    indicator_code VARCHAR(64) NOT NULL,
    period_code   VARCHAR(7) NOT NULL,          -- "2026-09"
    dimension_key VARCHAR(255) NOT NULL DEFAULT '',
    business_date DATE,
    value         NUMERIC(24,6),
    revision      INTEGER NOT NULL DEFAULT 1,
    source        VARCHAR(16) NOT NULL DEFAULT 'COMPUTED', -- COMPUTED|MANUAL|IMPORTED
    created_by    TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, indicator_code, period_code, dimension_key, revision)
);

CREATE INDEX IF NOT EXISTS idx_rpt_indicator_results_lookup
    ON rpt_indicator_results (tenant_id, period_code, indicator_code);

-- Cell-level audit borrowed from EPAS RPT_TXN_STAT_KPI_AUDIT: who changed a
-- computed/manual value, from what to what, and why.
CREATE TABLE IF NOT EXISTS rpt_indicator_audit (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    indicator_code VARCHAR(64) NOT NULL,
    period_code   VARCHAR(7) NOT NULL,
    dimension_key VARCHAR(255) NOT NULL DEFAULT '',
    old_value     NUMERIC(24,6),
    new_value     NUMERIC(24,6),
    reason        TEXT NOT NULL DEFAULT '',
    actor         TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_rpt_indicator_audit_lookup
    ON rpt_indicator_audit (tenant_id, period_code, indicator_code);

-- +goose Down
DROP TABLE IF EXISTS rpt_indicator_audit;
DROP TABLE IF EXISTS rpt_indicator_results;
ALTER TABLE rpt_indicators DROP CONSTRAINT IF EXISTS rpt_indicators_kpi_type_check;
ALTER TABLE rpt_indicators
    DROP COLUMN IF EXISTS kpi_type, DROP COLUMN IF EXISTS periodicity,
    DROP COLUMN IF EXISTS is_ratio, DROP COLUMN IF EXISTS root_code,
    DROP COLUMN IF EXISTS root_name, DROP COLUMN IF EXISTS meaning,
    DROP COLUMN IF EXISTS sources, DROP COLUMN IF EXISTS formula,
    DROP COLUMN IF EXISTS dimensions, DROP COLUMN IF EXISTS display_format,
    DROP COLUMN IF EXISTS rounding_digits, DROP COLUMN IF EXISTS score_group,
    DROP COLUMN IF EXISTS weight;
