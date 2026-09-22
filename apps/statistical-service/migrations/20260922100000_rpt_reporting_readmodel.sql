-- +goose Up

-- P3 reporting data layer (arda-be/docs/reporting-data-layer.md): the fact
-- read model the report builders query. statistical-service never reads a
-- domain database directly — a signed ETL job (RPT_EXTRACT_DAILY) materialises
-- the tenant slice for one business_date into these tables, idempotently
-- (delete + insert inside one transaction per date).
--
-- Grain: tenant x business_date x entity. Amounts are minor units. org_code is
-- carried because every report dimension (branch/địa bàn/PGD) hangs off it.

CREATE TABLE IF NOT EXISTS rpt_fact_loan_agreement_daily (
    tenant_id             VARCHAR(64) NOT NULL,
    business_date         DATE NOT NULL,
    org_code              VARCHAR(64) NOT NULL DEFAULT '',
    agreement_code        VARCHAR(64) NOT NULL,
    contract_code         VARCHAR(64) NOT NULL DEFAULT '',
    customer_code         VARCHAR(64) NOT NULL DEFAULT '',
    product_code          VARCHAR(64) NOT NULL DEFAULT '',
    disburse_date         DATE,
    maturity_date         DATE,
    debt_group_code       VARCHAR(32) NOT NULL DEFAULT '',
    status                VARCHAR(30) NOT NULL DEFAULT '',
    currency_code         VARCHAR(3)  NOT NULL DEFAULT 'VND',
    interest_rate         NUMERIC(9,6) NOT NULL DEFAULT 0,
    disburse_amt_minor    BIGINT NOT NULL DEFAULT 0,
    outstanding_amt_minor BIGINT NOT NULL DEFAULT 0,
    provision_amt_minor   BIGINT NOT NULL DEFAULT 0,
    extracted_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, agreement_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_loan_org
    ON rpt_fact_loan_agreement_daily (tenant_id, business_date, org_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_loan_debt_group
    ON rpt_fact_loan_agreement_daily (tenant_id, business_date, debt_group_code);

CREATE TABLE IF NOT EXISTS rpt_fact_deposit_contract_daily (
    tenant_id       VARCHAR(64) NOT NULL,
    business_date   DATE NOT NULL,
    org_code        VARCHAR(64) NOT NULL DEFAULT '',
    savings_code    VARCHAR(64) NOT NULL,
    customer_code   VARCHAR(64) NOT NULL DEFAULT '',
    product_code    VARCHAR(64) NOT NULL DEFAULT '',
    open_date       DATE,
    maturity_date   DATE,
    status          VARCHAR(16) NOT NULL DEFAULT '',
    currency_code   VARCHAR(3)  NOT NULL DEFAULT 'VND',
    principal_minor BIGINT NOT NULL DEFAULT 0,
    accrued_minor   BIGINT NOT NULL DEFAULT 0,
    extracted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, savings_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_deposit_org
    ON rpt_fact_deposit_contract_daily (tenant_id, business_date, org_code);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_deposit_product
    ON rpt_fact_deposit_contract_daily (tenant_id, business_date, product_code);

CREATE TABLE IF NOT EXISTS rpt_fact_loan_collateral_daily (
    tenant_id           VARCHAR(64) NOT NULL,
    business_date       DATE NOT NULL,
    org_code            VARCHAR(64) NOT NULL DEFAULT '',
    coll_code           VARCHAR(64) NOT NULL,
    coll_type_code      VARCHAR(32) NOT NULL DEFAULT '',
    valuation_date      DATE,
    status              VARCHAR(30) NOT NULL DEFAULT '',
    coll_value_minor    BIGINT NOT NULL DEFAULT 0,
    coll_use_value_minor BIGINT NOT NULL DEFAULT 0,
    extracted_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, coll_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_coll_type
    ON rpt_fact_loan_collateral_daily (tenant_id, business_date, coll_type_code);

CREATE TABLE IF NOT EXISTS rpt_fact_capital_contract_daily (
    tenant_id         VARCHAR(64) NOT NULL,
    business_date     DATE NOT NULL,
    org_code          VARCHAR(64) NOT NULL DEFAULT '',
    contract_code     VARCHAR(64) NOT NULL,
    fund_type_code    VARCHAR(64) NOT NULL DEFAULT '',
    counterparty_code VARCHAR(64) NOT NULL DEFAULT '',
    contract_date     DATE,
    amount_minor      BIGINT NOT NULL DEFAULT 0,
    interest_rate     NUMERIC(9,6) NOT NULL DEFAULT 0,
    currency_code     VARCHAR(3)  NOT NULL DEFAULT 'VND',
    status            VARCHAR(16) NOT NULL DEFAULT '',
    extracted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, contract_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_cap_fund_type
    ON rpt_fact_capital_contract_daily (tenant_id, business_date, fund_type_code);

CREATE TABLE IF NOT EXISTS rpt_fact_capital_movement_daily (
    tenant_id     VARCHAR(64) NOT NULL,
    business_date DATE NOT NULL,
    movement_id   VARCHAR(64) NOT NULL,
    contract_code VARCHAR(64) NOT NULL DEFAULT '',
    movement_type VARCHAR(32) NOT NULL DEFAULT '',
    movement_date DATE,
    amount_minor  BIGINT NOT NULL DEFAULT 0,
    currency_code VARCHAR(3)  NOT NULL DEFAULT 'VND',
    status        VARCHAR(16) NOT NULL DEFAULT '',
    extracted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, movement_id)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_cap_mv_type
    ON rpt_fact_capital_movement_daily (tenant_id, business_date, movement_type);

CREATE TABLE IF NOT EXISTS rpt_fact_customer_daily (
    tenant_id     VARCHAR(64) NOT NULL,
    business_date DATE NOT NULL,
    org_code      VARCHAR(64) NOT NULL DEFAULT '',
    customer_code VARCHAR(64) NOT NULL,
    customer_type VARCHAR(20) NOT NULL DEFAULT '',
    status        VARCHAR(50) NOT NULL DEFAULT '',
    segment       VARCHAR(64) NOT NULL DEFAULT '',
    customer_rank VARCHAR(64) NOT NULL DEFAULT '',
    risk_level    VARCHAR(64) NOT NULL DEFAULT '',
    extracted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_date, customer_code)
);
CREATE INDEX IF NOT EXISTS idx_rpt_fact_cust_segment
    ON rpt_fact_customer_daily (tenant_id, business_date, segment);

-- +goose Down
DROP TABLE IF EXISTS rpt_fact_customer_daily;
DROP TABLE IF EXISTS rpt_fact_capital_movement_daily;
DROP TABLE IF EXISTS rpt_fact_capital_contract_daily;
DROP TABLE IF EXISTS rpt_fact_loan_collateral_daily;
DROP TABLE IF EXISTS rpt_fact_deposit_contract_daily;
DROP TABLE IF EXISTS rpt_fact_loan_agreement_daily;
