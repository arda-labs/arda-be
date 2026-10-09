-- +goose Up

ALTER TABLE lnm_agreements
    ADD COLUMN off_bal_principal_minor BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN off_bal_interest_minor BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN off_bal_due_interest_minor BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN extension_date DATE,
    ADD COLUMN overdue_reason TEXT,
    ADD CONSTRAINT chk_lnm_agreements_off_bal_principal_nonnegative
        CHECK (off_bal_principal_minor >= 0),
    ADD CONSTRAINT chk_lnm_agreements_off_bal_interest_nonnegative
        CHECK (off_bal_interest_minor >= 0),
    ADD CONSTRAINT chk_lnm_agreements_off_bal_due_interest_nonnegative
        CHECK (off_bal_due_interest_minor >= 0);

CREATE TABLE lnm_journal_link (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           VARCHAR(64) NOT NULL,
    business_txn_type   VARCHAR(64) NOT NULL,
    business_txn_id     VARCHAR(64) NOT NULL,
    journal_entry_id    UUID NOT NULL,
    voucher_display_no  VARCHAR(128),
    posted_at           TIMESTAMPTZ NOT NULL,
    status              VARCHAR(16) NOT NULL CHECK (status IN ('POSTED', 'REVERSED')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_lnm_journal_link_business_txn UNIQUE (business_txn_type, business_txn_id),
    CONSTRAINT chk_lnm_journal_link_business_txn_type_nonempty CHECK (btrim(business_txn_type) <> ''),
    CONSTRAINT chk_lnm_journal_link_business_txn_id_nonempty CHECK (btrim(business_txn_id) <> '')
);
CREATE INDEX idx_lnm_journal_link_tenant_posted
    ON lnm_journal_link (tenant_id, posted_at DESC);

CREATE TABLE lnm_agreement_daily (
    tenant_id                    VARCHAR(64) NOT NULL,
    agreement_id                 VARCHAR(64) NOT NULL REFERENCES lnm_agreements(id),
    data_date                    DATE NOT NULL,
    currency_code                VARCHAR(8) NOT NULL,
    disburse_amt_minor           BIGINT NOT NULL,
    pending_disburse_amt_minor   BIGINT NOT NULL,
    outstanding_amt_minor        BIGINT NOT NULL,
    coln_principal_amt_minor     BIGINT NOT NULL,
    coln_interest_amt_minor      BIGINT NOT NULL,
    provision_amt_minor          BIGINT NOT NULL,
    off_bal_principal_minor      BIGINT NOT NULL,
    off_bal_interest_minor       BIGINT NOT NULL,
    off_bal_due_interest_minor   BIGINT NOT NULL,
    captured_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_lnm_agreement_daily PRIMARY KEY (agreement_id, data_date)
);
CREATE INDEX idx_lnm_agreement_daily_tenant_date
    ON lnm_agreement_daily (tenant_id, data_date, agreement_id);

-- +goose Down
DROP TABLE IF EXISTS lnm_agreement_daily;
DROP TABLE IF EXISTS lnm_journal_link;
ALTER TABLE lnm_agreements
    DROP CONSTRAINT IF EXISTS chk_lnm_agreements_off_bal_due_interest_nonnegative,
    DROP CONSTRAINT IF EXISTS chk_lnm_agreements_off_bal_interest_nonnegative,
    DROP CONSTRAINT IF EXISTS chk_lnm_agreements_off_bal_principal_nonnegative,
    DROP COLUMN IF EXISTS overdue_reason,
    DROP COLUMN IF EXISTS extension_date,
    DROP COLUMN IF EXISTS off_bal_due_interest_minor,
    DROP COLUMN IF EXISTS off_bal_interest_minor,
    DROP COLUMN IF EXISTS off_bal_principal_minor;
