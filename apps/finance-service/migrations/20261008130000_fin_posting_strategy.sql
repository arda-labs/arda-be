-- +goose Up

ALTER TABLE fin_accounting_rules
    ADD COLUMN posting_strategy TEXT NOT NULL DEFAULT 'SIMPLE';

ALTER TABLE fin_accounting_rules
    ADD CONSTRAINT chk_fin_accounting_rules_posting_strategy
    CHECK (posting_strategy IN ('SIMPLE', 'BAL_TYPE_SPLIT', 'DEBT_GROUP_RECLASS'));

-- Directed, effective-dated matrix entries. No business transitions are
-- seeded here; the task defines the mechanism without inventing mappings.
CREATE TABLE fin_debt_group_transitions (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         VARCHAR(64) NOT NULL,
    from_group_code   TEXT NOT NULL,
    to_group_code     TEXT NOT NULL,
    effective_from    DATE NOT NULL,
    effective_to      DATE,
    is_active         BOOLEAN NOT NULL DEFAULT true,
    is_deleted        BOOLEAN NOT NULL DEFAULT false,
    created_by        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    CHECK (from_group_code <> to_group_code),
    CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE UNIQUE INDEX uq_fin_debt_group_transitions_scope
    ON fin_debt_group_transitions (tenant_id, from_group_code, to_group_code, effective_from)
    WHERE is_deleted = false;
CREATE INDEX idx_fin_debt_group_transitions_effective
    ON fin_debt_group_transitions (tenant_id, effective_from, effective_to)
    WHERE is_active AND is_deleted = false;

-- +goose Down
DROP TABLE IF EXISTS fin_debt_group_transitions;
ALTER TABLE fin_accounting_rules
    DROP CONSTRAINT IF EXISTS chk_fin_accounting_rules_posting_strategy;
ALTER TABLE fin_accounting_rules DROP COLUMN IF EXISTS posting_strategy;
