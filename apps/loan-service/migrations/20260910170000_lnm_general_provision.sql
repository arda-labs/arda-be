-- +goose Up

-- LNM.307.01 general provision (per org, cumulative): rate config + period
-- rows. The approved row is the source of the cumulative provision for the
-- next period's delta (mirrors EPAS LNM_INF_PROVISION_GENERAL, single table).

CREATE TABLE IF NOT EXISTS lnm_general_provision_rates (
    org_code     VARCHAR(64) PRIMARY KEY,
    rate_percent NUMERIC(10,4) NOT NULL,
    updated_by   TEXT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- '%' is the fallback row (EPAS LNM_PROVISION_GEN_RATE fallback org '%').
INSERT INTO lnm_general_provision_rates (org_code, rate_percent)
VALUES ('%', 0.75)
ON CONFLICT (org_code) DO NOTHING;

CREATE TABLE IF NOT EXISTS lnm_general_provisions (
    id                      VARCHAR(64) PRIMARY KEY,
    tenant_id               VARCHAR(64) NOT NULL,
    org_code                VARCHAR(64) NOT NULL,
    provision_date          DATE NOT NULL,
    rate_percent            NUMERIC(10,4) NOT NULL,
    total_outstanding_minor BIGINT NOT NULL DEFAULT 0,
    accum_provision_minor   BIGINT NOT NULL DEFAULT 0,
    required_provision_minor BIGINT NOT NULL DEFAULT 0,
    alloc_minor             BIGINT NOT NULL DEFAULT 0,
    reverse_minor           BIGINT NOT NULL DEFAULT 0,
    status                  VARCHAR(32) NOT NULL,
    workflow_case_id        VARCHAR(64),
    workflow_case_code      VARCHAR(64),
    journal_entry_id        UUID,
    created_by              TEXT NOT NULL,
    updated_by              TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, org_code, provision_date)
);

CREATE INDEX IF NOT EXISTS lnm_general_provisions_tenant_org_idx
    ON lnm_general_provisions (tenant_id, org_code, provision_date DESC);

-- +goose Down
SELECT 1;
