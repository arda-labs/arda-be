-- +goose Up

-- Interbank BORROWING (tiền vay TCTD khác) — the mirror of ibm_deposits. The
-- QTDND borrows from the NHHTX/NHNN/another credit institution or the system
-- safety fund; the interbank market therefore lives in one service, with both
-- sides (placement and borrowing) under the same review.
--
-- Lifecycle mirrors the deposit side: a submission stages a PENDING_APPROVAL
-- borrow; a checker APPROVE moves it to ACTIVE. Movements (drawdown /
-- repayment / interest / early repayment) stage against an active borrow.
--
-- The PCF topic "Tiền vay TCTD" splits by lender (NHHTX / NHNN / other TCTD /
-- safety fund) and by funding purpose, so both are first-class columns rather
-- than derived: the source formulas distinguish them and a wrong bucket would
-- misstate the funding mix.

CREATE TABLE IF NOT EXISTS ibm_borrows (
    id                VARCHAR(64) PRIMARY KEY,
    tenant_id         VARCHAR(64) NOT NULL,
    borrow_code       VARCHAR(64) NOT NULL,
    counterparty_code VARCHAR(64) NOT NULL,
    counterparty_name VARCHAR(255) NOT NULL DEFAULT '',
    product_code      VARCHAR(64) NOT NULL DEFAULT '',
    -- NHHTX | NHNN | OTHER_TCTD | SAFETY_FUND
    lender_type       VARCHAR(24) NOT NULL DEFAULT 'OTHER_TCTD',
    -- CREDIT_EXPANSION | DEPOSIT_PAYMENT | DIFFICULTY | SPECIAL | OTHER
    funding_purpose   VARCHAR(32) NOT NULL DEFAULT 'OTHER',
    term_months       INTEGER NOT NULL DEFAULT 0,
    drawdown_date     DATE,
    maturity_date     DATE,
    principal_minor   BIGINT NOT NULL DEFAULT 0,
    outstanding_minor BIGINT NOT NULL DEFAULT 0,
    accrued_minor     BIGINT NOT NULL DEFAULT 0,
    interest_rate     NUMERIC(9,6) NOT NULL DEFAULT 0,
    currency_code     VARCHAR(3) NOT NULL DEFAULT 'VND',
    org_code          VARCHAR(64) NOT NULL DEFAULT '',
    status            VARCHAR(24) NOT NULL DEFAULT 'PENDING_APPROVAL',
    workflow_case_id  UUID,
    journal_entry_id  UUID,
    last_interest_date DATE,
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, borrow_code)
);

CREATE INDEX IF NOT EXISTS idx_ibm_borrow_tenant
    ON ibm_borrows (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_ibm_borrow_org
    ON ibm_borrows (tenant_id, org_code);

CREATE TABLE IF NOT EXISTS ibm_borrow_movements (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    borrow_id        VARCHAR(64) NOT NULL REFERENCES ibm_borrows(id),
    -- DRAWDOWN | REPAYMENT | INTEREST | EARLY_REPAY
    kind             VARCHAR(16) NOT NULL,
    amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code    VARCHAR(3) NOT NULL DEFAULT 'VND',
    movement_date    DATE NOT NULL,
    period_from      DATE,
    period_to        DATE,
    note             TEXT NOT NULL DEFAULT '',
    idempotency_key  VARCHAR(128),
    status           VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id UUID,
    journal_entry_id UUID,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    version          INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_ibm_borrow_movement_tenant
    ON ibm_borrow_movements (tenant_id, borrow_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_ibm_borrow_movement_idem
    ON ibm_borrow_movements (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS uq_ibm_borrow_movement_idem;
DROP INDEX IF EXISTS idx_ibm_borrow_movement_tenant;
DROP TABLE IF EXISTS ibm_borrow_movements;
DROP INDEX IF EXISTS idx_ibm_borrow_org;
DROP INDEX IF EXISTS idx_ibm_borrow_tenant;
DROP TABLE IF EXISTS ibm_borrows;
