-- +goose Up

-- REBUILD (Phase 0, goal 2026-09-07): the legacy fin_transactions ledger and
-- the internal maker-checker approvals are replaced end-to-end by the
-- PostingService journal (contract v0.2). Tables hold no production data —
-- drop outright, no dual-path.

DROP TABLE IF EXISTS fin_approval_steps;
DROP TABLE IF EXISTS fin_approval_requests;
DROP TABLE IF EXISTS fin_ledger_entries;
DROP TABLE IF EXISTS fin_account_balances;
DROP TABLE IF EXISTS fin_transactions;

-- +goose Down
-- No down: rebuild mode — the dropped schema is superseded, not replaced.
SELECT 1;
