-- +goose Up

-- T3.1: materialized account and daily balances carry an explicit balance
-- type. Existing Arda journal lines and counters only represent the actual
-- ledger; RESERVED is a posting lifecycle hold, not a second balance type.
ALTER TABLE fin_journal_lines
    ADD COLUMN bal_type_code TEXT;
UPDATE fin_journal_lines SET bal_type_code = 'ACTUAL' WHERE bal_type_code IS NULL;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM fin_journal_lines WHERE bal_type_code IS NULL OR btrim(bal_type_code) = '') THEN
        RAISE EXCEPTION 'T3.1 cannot backfill fin_journal_lines.bal_type_code';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE fin_journal_lines
    ALTER COLUMN bal_type_code SET NOT NULL,
    ADD CONSTRAINT fin_journal_lines_bal_type_nonempty CHECK (btrim(bal_type_code) <> '');
CREATE INDEX idx_fin_line_balance_key
    ON fin_journal_lines (tenant_id, bal_type_code, coa_version, account_code, currency_code);

ALTER TABLE fin_account_balances
    ADD COLUMN bal_type_code TEXT;
UPDATE fin_account_balances SET bal_type_code = 'ACTUAL' WHERE bal_type_code IS NULL;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM fin_account_balances WHERE bal_type_code IS NULL OR btrim(bal_type_code) = '') THEN
        RAISE EXCEPTION 'T3.1 cannot backfill fin_account_balances.bal_type_code';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE fin_account_balances
    ALTER COLUMN bal_type_code SET NOT NULL,
    DROP CONSTRAINT fin_account_balances_pkey,
    ADD PRIMARY KEY (tenant_id, coa_version, account_code, currency_code, bal_type_code),
    ADD CONSTRAINT fin_account_balances_bal_type_nonempty CHECK (btrim(bal_type_code) <> '');

ALTER TABLE fin_trial_balance_daily
    ADD COLUMN bal_type_code TEXT;
UPDATE fin_trial_balance_daily SET bal_type_code = 'ACTUAL' WHERE bal_type_code IS NULL;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM fin_trial_balance_daily WHERE bal_type_code IS NULL OR btrim(bal_type_code) = '') THEN
        RAISE EXCEPTION 'T3.1 cannot backfill fin_trial_balance_daily.bal_type_code';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE fin_trial_balance_daily
    ALTER COLUMN bal_type_code SET NOT NULL,
    DROP CONSTRAINT fin_trial_balance_daily_pkey,
    ADD PRIMARY KEY (tenant_id, business_date, org_code, bal_type_code, coa_version, account_code, currency_code),
    ADD CONSTRAINT fin_trial_balance_daily_bal_type_nonempty CHECK (btrim(bal_type_code) <> '');
CREATE INDEX idx_fin_tbd_balance_snapshot
    ON fin_trial_balance_daily (tenant_id, org_code, bal_type_code, coa_version, account_code, currency_code, business_date DESC);

-- +goose Down
-- No down: journal lines and daily snapshots are the canonical balance history.
SELECT 1;
