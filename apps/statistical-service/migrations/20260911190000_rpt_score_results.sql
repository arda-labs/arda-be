-- +goose Up

-- QCMS rank-score runtime (fe_statistical #19): persisted ranking results.
-- Criteria/weights come from mdm scoring catalogs and benchmarks from mdm
-- scoring-benchmarks; the compute endpoint accepts the resolved entries +
-- bands so statistical-service stays free of scoring SQL expressions
-- (the EPAS SQL_CALC_RESULT pitfall we deliberately dropped).

CREATE TABLE IF NOT EXISTS rpt_score_results (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         VARCHAR(64) NOT NULL,
    scoring_type_code VARCHAR(64) NOT NULL,
    subject_type      VARCHAR(32) NOT NULL DEFAULT 'CUSTOMER',
    subject_ref       VARCHAR(64) NOT NULL DEFAULT '',
    total_score       NUMERIC(9,4) NOT NULL DEFAULT 0,
    max_score         NUMERIC(9,4) NOT NULL DEFAULT 100,
    rank_code         VARCHAR(64) NOT NULL DEFAULT '',
    indicators        JSONB NOT NULL DEFAULT '[]',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_rpt_score_results_tenant
    ON rpt_score_results (tenant_id, scoring_type_code, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_rpt_score_results_subject
    ON rpt_score_results (tenant_id, subject_type, subject_ref);

-- +goose Down

DROP TABLE IF EXISTS rpt_score_results;
