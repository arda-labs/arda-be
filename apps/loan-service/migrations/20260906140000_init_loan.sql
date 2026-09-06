-- +goose Up

-- loan-service: credit domain (P1). Data model mirrors EPAS dev_lnm core
-- entities; EPAS snapshot tables (_a per-txn, _h per-day) are deliberate
-- omissions — balances live on agreements and are updated transactionally.

CREATE TABLE lnm_contracts (
    id                       VARCHAR(64) PRIMARY KEY,
    tenant_id                VARCHAR(64) NOT NULL DEFAULT '',
    contract_code            VARCHAR(64) NOT NULL,
    contract_no              VARCHAR(64),
    customer_code            VARCHAR(64) NOT NULL,
    employee_code            VARCHAR(64),
    contract_type_code       VARCHAR(32),
    product_code             VARCHAR(32),
    interest_rate            NUMERIC(9,6),
    interest_rate_type       VARCHAR(16),
    purpose_code             VARCHAR(32),
    industry_code            VARCHAR(32),
    loan_method_code         VARCHAR(32),
    contract_date            DATE,
    loan_term                INTEGER,
    term_unit                VARCHAR(16),
    maturity_date            DATE,
    interest_schedule_day    INTEGER,
    loan_amt_minor   BIGINT NOT NULL DEFAULT 0,
    interest_payment_freq    VARCHAR(16),
    principal_payment_freq   VARCHAR(16),
    interest_payment_method  VARCHAR(16),
    principal_payment_method VARCHAR(16),
    status                   VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id         VARCHAR(64),
    created_by               VARCHAR(64),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, contract_code)
);
CREATE INDEX lnm_contracts_status_idx ON lnm_contracts (tenant_id, status);

CREATE TABLE lnm_agreements (
    id                    VARCHAR(64) PRIMARY KEY,
    tenant_id             VARCHAR(64) NOT NULL DEFAULT '',
    contract_code         VARCHAR(64) NOT NULL,
    agreement_code        VARCHAR(64) NOT NULL,
    disburse_date         DATE,
    disburse_amt_minor   BIGINT NOT NULL DEFAULT 0,
    interest_rate         NUMERIC(9,6),
    over_interest_rate    NUMERIC(9,6),
    loan_term             INTEGER,
    term_unit             VARCHAR(16),
    maturity_date         DATE,
    debt_group_code       VARCHAR(32) NOT NULL DEFAULT 'GROUP_1',
    interest_payment_freq VARCHAR(16),
    principal_payment_freq VARCHAR(16),
    outstanding_amt_minor   BIGINT NOT NULL DEFAULT 0,
    coln_principal_amt_minor   BIGINT NOT NULL DEFAULT 0,
    coln_interest_amt_minor   BIGINT NOT NULL DEFAULT 0,
    provision_amt_minor   BIGINT NOT NULL DEFAULT 0,
    status                VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_by            VARCHAR(64),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, agreement_code)
);
CREATE INDEX lnm_agreements_contract_idx ON lnm_agreements (tenant_id, contract_code);

CREATE TABLE lnm_repay_plans (
    id                 VARCHAR(64) PRIMARY KEY,
    tenant_id          VARCHAR(64) NOT NULL DEFAULT '',
    contract_code      VARCHAR(64) NOT NULL,
    agreement_code     VARCHAR(64) NOT NULL,
    plan_no            INTEGER NOT NULL DEFAULT 1,
    term_no            INTEGER NOT NULL DEFAULT 1,
    from_date          DATE,
    to_date            DATE,
    interest_rate      NUMERIC(9,6),
    plan_principal_amt_minor   BIGINT NOT NULL DEFAULT 0,
    plan_interest_amt_minor   BIGINT NOT NULL DEFAULT 0,
    coln_principal_amt_minor   BIGINT NOT NULL DEFAULT 0,
    coln_interest_amt_minor   BIGINT NOT NULL DEFAULT 0,
    is_active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_repay_plans_agreement_idx ON lnm_repay_plans (tenant_id, agreement_code, term_no);

CREATE TABLE lnm_mortgages (
    id                VARCHAR(64) PRIMARY KEY,
    tenant_id         VARCHAR(64) NOT NULL DEFAULT '',
    mortgage_code     VARCHAR(64) NOT NULL,
    mortgage_no       VARCHAR(64),
    customer_code     VARCHAR(64) NOT NULL,
    mortgage_date     DATE,
    notarization_date DATE,
    registration_date DATE,
    expire_date       DATE,
    status            VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    description       TEXT,
    created_by        VARCHAR(64),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, mortgage_code)
);

CREATE TABLE lnm_collaterals (
    id              VARCHAR(64) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL DEFAULT '',
    coll_code       VARCHAR(64) NOT NULL,
    coll_name       VARCHAR(255) NOT NULL,
    coll_type_code  VARCHAR(32),
    mortgage_code   VARCHAR(64),
    owner_cif_code  VARCHAR(64),
    owner_name      VARCHAR(255),
    coll_address    VARCHAR(512),
    quantity        NUMERIC(20,4) NOT NULL DEFAULT 1,
    unit_price_minor BIGINT NOT NULL DEFAULT 0,
    coll_value_minor BIGINT NOT NULL DEFAULT 0,
    coll_use_value_minor BIGINT NOT NULL DEFAULT 0,
    valuation_date  DATE,
    status          VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_by      VARCHAR(64),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, coll_code)
);

CREATE TABLE lnm_contract_collaterals (
    id            VARCHAR(64) PRIMARY KEY,
    tenant_id     VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL,
    coll_code     VARCHAR(64) NOT NULL,
    coll_value_minor BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, contract_code, coll_code)
);

-- One uniform table per adjustment flow (kind → table in
-- repository.AdjustmentTables). Payload carries flow-specific fields,
-- validated per kind in the service layer.
CREATE TABLE lnm_debt_changes (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_debt_changes_lookup_idx ON lnm_debt_changes (tenant_id, contract_code, status);

CREATE TABLE lnm_rate_changes (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_rate_changes_lookup_idx ON lnm_rate_changes (tenant_id, contract_code, status);

CREATE TABLE lnm_restructures (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_restructures_lookup_idx ON lnm_restructures (tenant_id, contract_code, status);

CREATE TABLE lnm_waivers (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_waivers_lookup_idx ON lnm_waivers (tenant_id, contract_code, status);

CREATE TABLE lnm_writeoffs (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_writeoffs_lookup_idx ON lnm_writeoffs (tenant_id, contract_code, status);

CREATE TABLE lnm_recoveries (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_recoveries_lookup_idx ON lnm_recoveries (tenant_id, contract_code, status);

CREATE TABLE lnm_fund_checks (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_fund_checks_lookup_idx ON lnm_fund_checks (tenant_id, contract_code, status);

CREATE TABLE lnm_revenue_allocations (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_revenue_allocations_lookup_idx ON lnm_revenue_allocations (tenant_id, contract_code, status);

CREATE TABLE lnm_vfu_fee_allocations (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_vfu_fee_allocations_lookup_idx ON lnm_vfu_fee_allocations (tenant_id, contract_code, status);

CREATE TABLE lnm_off_balance_exports (
    id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    contract_code VARCHAR(64) NOT NULL, agreement_code VARCHAR(64),
    effective_date DATE, amount_minor  BIGINT,
    payload JSONB, status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id VARCHAR(64), decision_note TEXT, decided_by VARCHAR(64),
    created_by VARCHAR(64), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lnm_off_balance_exports_lookup_idx ON lnm_off_balance_exports (tenant_id, contract_code, status);

-- +goose Down
DROP TABLE IF EXISTS lnm_off_balance_exports;
DROP TABLE IF EXISTS lnm_vfu_fee_allocations;
DROP TABLE IF EXISTS lnm_revenue_allocations;
DROP TABLE IF EXISTS lnm_fund_checks;
DROP TABLE IF EXISTS lnm_recoveries;
DROP TABLE IF EXISTS lnm_writeoffs;
DROP TABLE IF EXISTS lnm_waivers;
DROP TABLE IF EXISTS lnm_restructures;
DROP TABLE IF EXISTS lnm_rate_changes;
DROP TABLE IF EXISTS lnm_debt_changes;
DROP TABLE IF EXISTS lnm_contract_collaterals;
DROP TABLE IF EXISTS lnm_collaterals;
DROP TABLE IF EXISTS lnm_mortgages;
DROP TABLE IF EXISTS lnm_repay_plans;
DROP TABLE IF EXISTS lnm_agreements;
DROP TABLE IF EXISTS lnm_contracts;
