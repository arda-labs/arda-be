-- +goose Up

-- LNM.306 specific provision (W7): per-agreement provision with collateral
-- deduction (deduction_ratio snapshot defaults 0 until business confirms —
-- loan-provision-deep-dive §7), staged through a maker/checker case.

ALTER TABLE lnm_collaterals ADD COLUMN IF NOT EXISTS deduction_ratio NUMERIC(9,6) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS lnm_specific_provisions (
    id                 VARCHAR(64) PRIMARY KEY,
    tenant_id          VARCHAR(64) NOT NULL,
    contract_code      VARCHAR(64) NOT NULL,
    agreement_code     VARCHAR(64) NOT NULL,
    provision_date     DATE NOT NULL,
    outstanding_minor  BIGINT NOT NULL,
    debt_group_code    VARCHAR(32) NOT NULL,
    rate_percent       NUMERIC(9,6) NOT NULL,
    deduction_minor    BIGINT NOT NULL DEFAULT 0,
    base_minor         BIGINT NOT NULL DEFAULT 0,
    amount_minor       BIGINT NOT NULL DEFAULT 0,
    status             VARCHAR(16) NOT NULL DEFAULT 'SUBMITTED', -- SUBMITTED|POSTED|REJECTED
    workflow_case_id   UUID,
    workflow_case_code VARCHAR(80),
    journal_entry_id   UUID,
    created_by         TEXT NOT NULL,
    updated_by         TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    version            INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, agreement_code, provision_date)
);

CREATE INDEX IF NOT EXISTS idx_lnm_specific_provision_tenant
    ON lnm_specific_provisions (tenant_id, status, provision_date DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_lnm_specific_provision_tenant;
DROP TABLE IF EXISTS lnm_specific_provisions;
ALTER TABLE lnm_collaterals DROP COLUMN IF EXISTS deduction_ratio;
