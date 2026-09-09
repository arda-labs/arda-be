-- +goose Up

-- Iteration 12 — trader stamp: free-form entry-level metadata the callers
-- (workflow cancellation/manual-posting workers) stamp onto journal entries.
-- Fixed keys today: actor + trader_* (trader_object_type, trader_object_code,
-- trader_object_name, trader_id_number, trader_issue_date, trader_issue_place,
-- trader_address). NULL for entries posted before this column existed.

ALTER TABLE fin_journal_entries
    ADD COLUMN IF NOT EXISTS metadata JSONB;

COMMENT ON COLUMN fin_journal_entries.metadata IS
    'Caller-stamped entry metadata (actor + trader_* keys); NULL when absent';

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
