-- +goose Up

CREATE TABLE IF NOT EXISTS fin_approval_requests (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    request_type TEXT NOT NULL,               -- TRANSFER, DEPOSIT, WITHDRAWAL, ACCOUNT_OPENING
    ref_id UUID NOT NULL,                    -- ID của transaction cần approve
    status TEXT NOT NULL DEFAULT 'PENDING',  -- PENDING, PENDING_L2, PENDING_L3, APPROVED, REJECTED, CANCELLED
    current_level INT NOT NULL DEFAULT 1,
    total_levels INT NOT NULL DEFAULT 2,
    maker_id UUID NOT NULL,
    maker_note TEXT,
    amount NUMERIC(26,6),
    currency TEXT DEFAULT 'VND',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    metadata JSONB
);

CREATE INDEX IF NOT EXISTS idx_approval_status ON fin_approval_requests(status, tenant_id);
CREATE INDEX IF NOT EXISTS idx_approval_ref ON fin_approval_requests(ref_id);
CREATE INDEX IF NOT EXISTS idx_approval_maker ON fin_approval_requests(maker_id);

CREATE TABLE IF NOT EXISTS fin_approval_steps (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    request_id UUID NOT NULL REFERENCES fin_approval_requests(id) ON DELETE CASCADE,
    level INT NOT NULL,
    checker_id UUID NOT NULL,
    decision TEXT,                           -- APPROVED, REJECTED
    note TEXT,
    decided_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(request_id, level)
);

CREATE INDEX IF NOT EXISTS idx_approval_steps_request ON fin_approval_steps(request_id);
CREATE INDEX IF NOT EXISTS idx_approval_steps_checker ON fin_approval_steps(checker_id);

-- +goose Down
DROP TABLE IF EXISTS fin_approval_steps CASCADE;
DROP TABLE IF EXISTS fin_approval_requests CASCADE;
