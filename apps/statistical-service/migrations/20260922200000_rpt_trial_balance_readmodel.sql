-- +goose Up

-- Reporting read model for the accounting indicators (step 6c): the statistical
-- ETL materialises finance-service's fin_trial_balance_daily into its own fact
-- so the PCF "Tài chính kế toán" indicators (322 rows: balance sheet, income
-- statement, taxes, equity) resolve without statistical-service ever reading
-- the finance database.
--
-- Grain: tenant x business_date x account_code x currency x org_code. The
-- account_code prefix is the mapping key the indicators use (TK 10 = cash,
-- TK 21 = customer loans, ...), so it is kept verbatim — never normalised.
--
-- close_debit/close_credit are the end-of-day balances in minor units; readers
-- derive the net as close_debit - close_credit. Both are kept because some
-- indicators read the credit side directly (DCC = dư cuối có).

CREATE TABLE IF NOT EXISTS rpt_fact_trial_balance_daily (
    tenant_id           VARCHAR(64) NOT NULL,
    business_date       DATE NOT NULL,
    org_code            VARCHAR(64) NOT NULL DEFAULT '',
    coa_version         VARCHAR(64) NOT NULL DEFAULT '',
    account_code        VARCHAR(32) NOT NULL,
    account_name        VARCHAR(255) NOT NULL DEFAULT '',
    currency_code       VARCHAR(8) NOT NULL DEFAULT 'VND',
    open_debit_minor    BIGINT NOT NULL DEFAULT 0,
    open_credit_minor   BIGINT NOT NULL DEFAULT 0,
    incr_debit_minor    BIGINT NOT NULL DEFAULT 0,
    incr_credit_minor   BIGINT NOT NULL DEFAULT 0,
    close_debit_minor   BIGINT NOT NULL DEFAULT 0,
    close_credit_minor  BIGINT NOT NULL DEFAULT 0,
    extracted_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, account_code, currency_code, org_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_tb_account
    ON rpt_fact_trial_balance_daily (tenant_id, business_date, account_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_tb_org
    ON rpt_fact_trial_balance_daily (tenant_id, business_date, org_code);

-- +goose Down
DROP TABLE IF EXISTS rpt_fact_trial_balance_daily;
