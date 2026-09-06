-- +goose Up

-- P1b.4a: loan collections — principal + interest receipts against an
-- agreement, approved through the workflow and posted via PostingService
-- (docs/accounting-rule-cards.md LNM_COLLECTION, 4 lines).

CREATE TABLE IF NOT EXISTS lnm_collections (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           VARCHAR(64) NOT NULL,
    contract_code       VARCHAR(64) NOT NULL,
    agreement_code      VARCHAR(64) NOT NULL,
    collection_date     DATE NOT NULL,
    principal_minor     BIGINT NOT NULL DEFAULT 0 CHECK (principal_minor >= 0),
    interest_minor      BIGINT NOT NULL DEFAULT 0 CHECK (interest_minor >= 0),
    currency_code       VARCHAR(3) NOT NULL DEFAULT 'VND',
    status              VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    payload             JSONB,
    workflow_case_id    UUID,
    journal_entry_id    UUID,
    created_by          TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          TEXT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    version             INTEGER NOT NULL DEFAULT 1,
    CHECK (principal_minor + interest_minor > 0)
);

CREATE INDEX IF NOT EXISTS idx_lnm_col_tenant ON lnm_collections (tenant_id, contract_code);
CREATE UNIQUE INDEX IF NOT EXISTS uq_lnm_col_case ON lnm_collections (tenant_id, workflow_case_id)
    WHERE workflow_case_id IS NOT NULL;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
