-- +goose Up

-- Reporting read model for interbank deposits (step 6b): the statistical ETL
-- materialises deposit-service's ibm_deposits for one business_date so the
-- PCF topic "Tiền gửi TCTD" indicators resolve without statistical-service ever
-- reading the deposit database.
--
-- Grain: tenant x business_date x deposit_code. One row per interbank deposit
-- contract with its outstanding principal, term and rate as of the business
-- date.
--
-- Counterparty classification (NHHT / NHNN / other credit institution) is NOT
-- hardcoded here: it belongs to the credit-institution registry
-- (platform plt_credit_institutions). The fact keeps counterparty_code so a
-- "by counterparty" slice works today, and a later classification column can be
-- added without reshaping the metrics.

CREATE TABLE IF NOT EXISTS rpt_fact_ibm_deposit_daily (
    tenant_id          VARCHAR(64) NOT NULL,
    business_date      DATE NOT NULL,
    org_code           VARCHAR(64) NOT NULL DEFAULT '',
    deposit_code       VARCHAR(64) NOT NULL,
    counterparty_code  VARCHAR(64) NOT NULL DEFAULT '',
    product_code       VARCHAR(64) NOT NULL DEFAULT '',
    -- term_months 0 = demand (không kỳ hạn); the indicator split 60002.02/03
    -- reads this.
    term_months        INTEGER NOT NULL DEFAULT 0,
    deposit_date       DATE,
    maturity_date      DATE,
    status             VARCHAR(24) NOT NULL DEFAULT '',
    currency_code      VARCHAR(8) NOT NULL DEFAULT 'VND',
    principal_minor    BIGINT NOT NULL DEFAULT 0,
    accrued_minor      BIGINT NOT NULL DEFAULT 0,
    interest_rate      NUMERIC(9,4) NOT NULL DEFAULT 0,
    extracted_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, deposit_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_ibm_deposit_org
    ON rpt_fact_ibm_deposit_daily (tenant_id, business_date, org_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_ibm_deposit_counterparty
    ON rpt_fact_ibm_deposit_daily (tenant_id, business_date, counterparty_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_ibm_deposit_term
    ON rpt_fact_ibm_deposit_daily (tenant_id, business_date, term_months);

-- +goose Down
DROP TABLE IF EXISTS rpt_fact_ibm_deposit_daily;
