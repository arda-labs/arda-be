-- +goose Up

-- P1b residue: mortgage adjustment flow (11th adjustment kind) — adjust
-- collateral allocations on a mortgage (increase/release/collateral swap).

CREATE TABLE IF NOT EXISTS lnm_mortgage_adjustments (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL,
    contract_code  VARCHAR(64) NOT NULL,
    mortgage_code  VARCHAR(64) NOT NULL,
    effective_date DATE,
    payload        JSONB,
    status         VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    workflow_case_id UUID,
    decision_note  TEXT,
    decided_by     TEXT,
    created_by     TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lnm_mortgage_adj_tenant ON lnm_mortgage_adjustments (tenant_id, contract_code);

-- +goose Down
SELECT 1;
