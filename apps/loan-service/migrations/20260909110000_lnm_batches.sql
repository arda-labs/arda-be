-- +goose Up

-- Iteration 13 wave BE: batch (1 hồ sơ — N hợp đồng) disbursement/collection
-- per EPAS, mirroring the finance two-phase Reserve/Post/Release lifecycle.
-- A batch is the workflow case document; its rows are the per-agreement
-- lnm_disbursements / lnm_collections legs already settled by the existing
-- per-row path (kept working — batch_id is nullable on the legacy tables).

CREATE TABLE IF NOT EXISTS lnm_disbursement_batches (
    id                 UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          VARCHAR(64) NOT NULL,
    org_code           VARCHAR(32) NOT NULL DEFAULT '',
    flow_type          VARCHAR(10) NOT NULL CHECK (flow_type IN ('REGISTER', 'COMPLETE')),
    source_batch_id    UUID,
    txn_date           DATE NOT NULL,
    payment_method     VARCHAR(16) NOT NULL DEFAULT 'TRANSFER',
    account_code       VARCHAR(64) NOT NULL DEFAULT '',
    currency_code      VARCHAR(3) NOT NULL DEFAULT 'VND',
    total_amt_minor    BIGINT NOT NULL DEFAULT 0,
    description        TEXT NOT NULL DEFAULT '',
    trader             JSONB NOT NULL DEFAULT '{}'::jsonb,
    status             VARCHAR(16) NOT NULL DEFAULT 'DRAFT'
                       CHECK (status IN ('DRAFT', 'SUBMITTED', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED')),
    workflow_case_id   UUID,
    workflow_case_code VARCHAR(64) NOT NULL DEFAULT '',
    journal_entry_id   UUID,
    created_by         VARCHAR(64),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    version            INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_lnm_disb_batch_tenant ON lnm_disbursement_batches (tenant_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_lnm_disb_batch_case ON lnm_disbursement_batches (tenant_id, workflow_case_id)
    WHERE workflow_case_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS lnm_collection_batches (
    id                    UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id             VARCHAR(64) NOT NULL,
    org_code              VARCHAR(32) NOT NULL DEFAULT '',
    txn_date              DATE NOT NULL,
    payment_method        VARCHAR(16) NOT NULL DEFAULT 'TRANSFER',
    account_code          VARCHAR(64) NOT NULL DEFAULT '',
    currency_code         VARCHAR(3) NOT NULL DEFAULT 'VND',
    total_principal_minor BIGINT NOT NULL DEFAULT 0,
    total_interest_minor  BIGINT NOT NULL DEFAULT 0,
    description           TEXT NOT NULL DEFAULT '',
    trader                JSONB NOT NULL DEFAULT '{}'::jsonb,
    status                VARCHAR(16) NOT NULL DEFAULT 'DRAFT'
                          CHECK (status IN ('DRAFT', 'SUBMITTED', 'APPROVED', 'REJECTED', 'CANCELLED', 'POSTED')),
    workflow_case_id      UUID,
    workflow_case_code    VARCHAR(64) NOT NULL DEFAULT '',
    journal_entry_id      UUID,
    created_by            VARCHAR(64),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    version               INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_lnm_col_batch_tenant ON lnm_collection_batches (tenant_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_lnm_col_batch_case ON lnm_collection_batches (tenant_id, workflow_case_id)
    WHERE workflow_case_id IS NOT NULL;

-- Batch rows: the existing per-row tables gain a nullable batch_id, so the
-- legacy single-row path (no batch_id) keeps working untouched.
ALTER TABLE lnm_disbursements ADD COLUMN IF NOT EXISTS batch_id UUID;
CREATE INDEX IF NOT EXISTS idx_lnm_disb_batch ON lnm_disbursements (tenant_id, batch_id);

ALTER TABLE lnm_collections ADD COLUMN IF NOT EXISTS batch_id UUID;
CREATE INDEX IF NOT EXISTS idx_lnm_col_batch ON lnm_collections (tenant_id, batch_id);

ALTER TABLE lnm_collections ADD COLUMN IF NOT EXISTS overdue_interest_minor BIGINT NOT NULL DEFAULT 0;
-- A receipt row may now carry only overdue interest (principal = interest = 0,
-- overdue > 0) — the table CHECK is widened accordingly. Legacy rows all have
-- principal + interest > 0, so the single-row path is unaffected.
-- +goose StatementBegin
DO $$
DECLARE
    old_check TEXT;
BEGIN
    SELECT conname INTO old_check
    FROM pg_constraint
    WHERE conrelid = 'lnm_collections'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%interest_minor > 0%'
    LIMIT 1;
    IF old_check IS NOT NULL THEN
        EXECUTE format('ALTER TABLE lnm_collections DROP CONSTRAINT %I', old_check);
        ALTER TABLE lnm_collections ADD CONSTRAINT lnm_collections_positive_amt_check
            CHECK (principal_minor + interest_minor + overdue_interest_minor > 0);
    END IF;
END $$;
-- +goose StatementEnd

-- is_closed rows (batch COMPLETE) carry amount 0 — they close the contract
-- after settle without moving cash (posting legs skip zero amounts). The
-- legacy disburse_amt_minor > 0 CHECK is relaxed to >= 0; legacy rows all
-- carry positive amounts, so the single-row path is unaffected.
ALTER TABLE lnm_disbursements ADD COLUMN IF NOT EXISTS is_closed BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementBegin
DO $$
DECLARE
    old_check TEXT;
BEGIN
    SELECT conname INTO old_check
    FROM pg_constraint
    WHERE conrelid = 'lnm_disbursements'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%disburse_amt_minor%>%0%'
      AND NOT pg_get_constraintdef(oid) LIKE '%>= 0%'
    LIMIT 1;
    IF old_check IS NOT NULL THEN
        EXECUTE format('ALTER TABLE lnm_disbursements DROP CONSTRAINT %I', old_check);
        ALTER TABLE lnm_disbursements ADD CONSTRAINT lnm_disbursements_disburse_amt_minor_check CHECK (disburse_amt_minor >= 0);
    END IF;
END $$;
-- +goose StatementEnd

-- EPAS group key (plan_code): data is loaded at runtime, no seed here.
ALTER TABLE lnm_agreements ADD COLUMN IF NOT EXISTS plan_code VARCHAR(64) NOT NULL DEFAULT '';

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
