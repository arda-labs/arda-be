-- +goose Up

-- Agreements carry the product's accounting classification so disbursement
-- postings can resolve the COA account via finance /coa/resolve.
ALTER TABLE lnm_agreements ADD COLUMN IF NOT EXISTS acc_classification VARCHAR(64) NOT NULL DEFAULT '';

-- VFU (ủy thác) core entities — EPAS lnm_inf_vfu_* consolidated: parties,
-- mandate contracts, funding plans. Appraisal workflow tables (lnm_txn_vfu_ap*)
-- are covered by the workflow engine instead.
CREATE TABLE lnm_vfu_parties (
    id                   VARCHAR(64) PRIMARY KEY,
    tenant_id            VARCHAR(64) NOT NULL DEFAULT '',
    party_code           VARCHAR(64) NOT NULL,
    party_name           VARCHAR(255) NOT NULL,
    party_type           VARCHAR(16) NOT NULL DEFAULT 'ORG',   -- ORG, PERSON
    gender_code          VARCHAR(8),
    date_of_birth        DATE,
    identification_id    VARCHAR(64),
    issue_date           DATE,
    issue_place          VARCHAR(255),
    mobile_number        VARCHAR(32),
    permanent_address    VARCHAR(512),
    customer_reln_code   VARCHAR(32),
    status               VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_by           VARCHAR(64),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, party_code)
);

CREATE TABLE lnm_vfu_mandates (
    id                  VARCHAR(64) PRIMARY KEY,
    tenant_id           VARCHAR(64) NOT NULL DEFAULT '',
    mandate_code        VARCHAR(64) NOT NULL,
    mandate_no          VARCHAR(64),
    mandate_date        DATE,
    party_code          VARCHAR(64) NOT NULL,
    org_code            VARCHAR(64),
    rep_name            VARCHAR(255),
    rep_phone           VARCHAR(32),
    rep_address         VARCHAR(512),
    bank_name           VARCHAR(255),
    bank_account        VARCHAR(64),
    fee_payment_freq    VARCHAR(16),
    rate_value          NUMERIC(9,6),
    status              VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_by          VARCHAR(64),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, mandate_code)
);

CREATE TABLE lnm_vfu_plans (
    id               VARCHAR(64) PRIMARY KEY,
    tenant_id        VARCHAR(64) NOT NULL DEFAULT '',
    plan_code        VARCHAR(64) NOT NULL,
    plan_date        DATE,
    mandate_code     VARCHAR(64) NOT NULL,
    contract_code    VARCHAR(64),
    allocated_amt_minor   BIGINT NOT NULL DEFAULT 0,
    settled_amt_minor   BIGINT NOT NULL DEFAULT 0,
    fee_amt_minor   BIGINT NOT NULL DEFAULT 0,
    status           VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_by       VARCHAR(64),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, plan_code)
);

-- +goose Down
DROP TABLE IF EXISTS lnm_vfu_plans;
DROP TABLE IF EXISTS lnm_vfu_mandates;
DROP TABLE IF EXISTS lnm_vfu_parties;
ALTER TABLE lnm_agreements DROP COLUMN IF EXISTS acc_classification;
