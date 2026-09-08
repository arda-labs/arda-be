-- +goose Up

-- P1b v2 two-phase balance (docs/epas-survey/disbursement-flow-deep-dive.md):
-- materialized per-account-per-currency counters, checked atomically at
-- reserve time — the EPAS FacInfAcc balAvailable/balActual pattern, also
-- TigerBeetle debits/credits_pending and Midaz available/on_hold.
-- posted_* move only on POSTED entries (direct posts and reversals);
-- reserved_* are held by PENDING entries and freed on Post (moved to
-- posted) or Release (VOID). Opening balances stay journal-independent
-- (P1a design); availability math combines both in balance_repo/service.

CREATE TABLE IF NOT EXISTS fin_account_balances (
    tenant_id             VARCHAR(64) NOT NULL,
    coa_version           TEXT NOT NULL,
    account_code          TEXT NOT NULL,
    currency_code         VARCHAR(3) NOT NULL,
    posted_debit_minor    BIGINT NOT NULL DEFAULT 0 CHECK (posted_debit_minor >= 0),
    posted_credit_minor   BIGINT NOT NULL DEFAULT 0 CHECK (posted_credit_minor >= 0),
    reserved_debit_minor  BIGINT NOT NULL DEFAULT 0 CHECK (reserved_debit_minor >= 0),
    reserved_credit_minor BIGINT NOT NULL DEFAULT 0 CHECK (reserved_credit_minor >= 0),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    version               INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, coa_version, account_code, currency_code)
);

ALTER TABLE fin_journal_entries ADD COLUMN IF NOT EXISTS posted_at TIMESTAMPTZ;
ALTER TABLE fin_journal_entries ADD COLUMN IF NOT EXISTS void_reason TEXT;
-- status gains PENDING (reserve held, not yet on posted balances) and VOID
-- (reserve released before posting); POSTED | REVERSED unchanged.
UPDATE fin_journal_entries SET posted_at = created_at WHERE status = 'POSTED' AND posted_at IS NULL;

-- Backfill posted counters from live POSTED entries. Originals marked
-- REVERSED are excluded; their reversal entries are POSTED and net the
-- counters out to zero for the reversed pair.
INSERT INTO fin_account_balances
    (tenant_id, coa_version, account_code, currency_code, posted_debit_minor, posted_credit_minor)
SELECT l.tenant_id, l.coa_version, l.account_code, l.currency_code,
       COALESCE(SUM(l.amount_minor) FILTER (WHERE l.direction = 'DEBIT'), 0),
       COALESCE(SUM(l.amount_minor) FILTER (WHERE l.direction = 'CREDIT'), 0)
FROM fin_journal_lines l
JOIN fin_journal_entries e ON e.tenant_id = l.tenant_id AND e.id = l.entry_id
WHERE e.status = 'POSTED'
GROUP BY l.tenant_id, l.coa_version, l.account_code, l.currency_code
ON CONFLICT (tenant_id, coa_version, account_code, currency_code) DO NOTHING;

-- +goose Down
-- No down: rebuild mode — superseding schema, not replaceable.
SELECT 1;
