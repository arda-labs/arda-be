-- +goose Up

-- P1b.2a: loan disbursements — one drawdown posting per agreement, approved
-- through the workflow and settled via the finance PostingService
-- (docs/accounting-rule-cards.md LNM_DISBURSEMENT).

CREATE TABLE IF NOT EXISTS lnm_disbursements (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           VARCHAR(64) NOT NULL,
    contract_code       VARCHAR(64) NOT NULL,
    agreement_code      VARCHAR(64) NOT NULL,
    disburse_date       DATE NOT NULL,
    disburse_amt_minor  BIGINT NOT NULL CHECK (disburse_amt_minor > 0),
    currency_code       VARCHAR(3) NOT NULL DEFAULT 'VND',
    fund_source_code    VARCHAR(64),
    status              VARCHAR(16) NOT NULL DEFAULT 'DRAFT', -- DRAFT|SUBMITTED|APPROVED|REJECTED|CANCELLED|POSTED
    payload             JSONB,
    workflow_case_id    UUID,
    journal_entry_id    UUID,
    created_by          TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          TEXT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    version             INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_lnm_disb_tenant ON lnm_disbursements (tenant_id, contract_code);
CREATE UNIQUE INDEX IF NOT EXISTS uq_lnm_disb_case ON lnm_disbursements (tenant_id, workflow_case_id)
    WHERE workflow_case_id IS NOT NULL;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
