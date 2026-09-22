-- +goose Up

-- Reporting read model for interbank BORROWING (step 6b-2): the statistical ETL
-- materialises deposit-service's ibm_borrows for one business_date so the PCF
-- topic "Tiền vay TCTD" resolves without statistical-service ever reading the
-- deposit database.
--
-- Grain: tenant x business_date x borrow_code.
--
-- maturity_status is computed by the ETL (maturity_date vs the business date)
-- rather than left to the indicator, because the engine's filter language
-- compares bound literals, not dates: the "nợ trong hạn / quá hạn" split needs
-- a value it can test with a plain equality.

CREATE TABLE IF NOT EXISTS rpt_fact_ibm_borrow_daily (
    tenant_id         VARCHAR(64) NOT NULL,
    business_date     DATE NOT NULL,
    org_code          VARCHAR(64) NOT NULL DEFAULT '',
    borrow_code       VARCHAR(64) NOT NULL,
    counterparty_code VARCHAR(64) NOT NULL DEFAULT '',
    -- NHHTX | NHNN | OTHER_TCTD | SAFETY_FUND
    lender_type       VARCHAR(24) NOT NULL DEFAULT '',
    -- CREDIT_EXPANSION | DEPOSIT_PAYMENT | DIFFICULTY | SPECIAL | OTHER
    funding_purpose   VARCHAR(32) NOT NULL DEFAULT '',
    term_months       INTEGER NOT NULL DEFAULT 0,
    drawdown_date     DATE,
    maturity_date     DATE,
    -- CURRENT | OVERDUE | SETTLED
    maturity_status   VARCHAR(16) NOT NULL DEFAULT '',
    status            VARCHAR(24) NOT NULL DEFAULT '',
    currency_code     VARCHAR(8) NOT NULL DEFAULT 'VND',
    principal_minor   BIGINT NOT NULL DEFAULT 0,
    outstanding_minor BIGINT NOT NULL DEFAULT 0,
    accrued_minor     BIGINT NOT NULL DEFAULT 0,
    interest_rate     NUMERIC(9,4) NOT NULL DEFAULT 0,
    extracted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, borrow_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_ibm_borrow_org
    ON rpt_fact_ibm_borrow_daily (tenant_id, business_date, org_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_ibm_borrow_lender
    ON rpt_fact_ibm_borrow_daily (tenant_id, business_date, lender_type);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_ibm_borrow_purpose
    ON rpt_fact_ibm_borrow_daily (tenant_id, business_date, funding_purpose);

-- +goose Down
DROP TABLE IF EXISTS rpt_fact_ibm_borrow_daily;
