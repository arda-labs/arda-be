-- +goose Up
CREATE TABLE IF NOT EXISTS fin_journal_lines (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    journal_definition_id UUID NOT NULL REFERENCES fin_journal_definitions(id) ON DELETE CASCADE,
    line_seq INT NOT NULL,
    entry_type VARCHAR(16) NOT NULL,
    account_resolution_type TEXT NOT NULL DEFAULT 'FIXED_CODE',
    account_ref TEXT NOT NULL,
    amount_source TEXT NOT NULL DEFAULT 'TRANSACTION_AMOUNT',
    description_template TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(journal_definition_id, line_seq)
);

CREATE INDEX IF NOT EXISTS idx_fin_journal_lines_definition
    ON fin_journal_lines(journal_definition_id, line_seq);

INSERT INTO fin_journal_lines (
    journal_definition_id, line_seq, entry_type, account_resolution_type,
    account_ref, amount_source, description_template
)
SELECT id, 1, 'DEBIT', 'FIXED_CODE', debit_account_code, amount_source, description_template
FROM fin_journal_definitions
ON CONFLICT (journal_definition_id, line_seq) DO NOTHING;

INSERT INTO fin_journal_lines (
    journal_definition_id, line_seq, entry_type, account_resolution_type,
    account_ref, amount_source, description_template
)
SELECT id, 2, 'CREDIT', 'FIXED_CODE', credit_account_code, amount_source, description_template
FROM fin_journal_definitions
ON CONFLICT (journal_definition_id, line_seq) DO NOTHING;

ALTER TABLE fin_journal_definitions DROP COLUMN IF EXISTS debit_account_code;
ALTER TABLE fin_journal_definitions DROP COLUMN IF EXISTS credit_account_code;

-- +goose Down
ALTER TABLE fin_journal_definitions ADD COLUMN IF NOT EXISTS debit_account_code TEXT;
ALTER TABLE fin_journal_definitions ADD COLUMN IF NOT EXISTS credit_account_code TEXT;

UPDATE fin_journal_definitions d
SET
    debit_account_code = dr.account_ref,
    credit_account_code = cr.account_ref
FROM fin_journal_lines dr
JOIN fin_journal_lines cr
    ON cr.journal_definition_id = dr.journal_definition_id
   AND cr.line_seq = 2
   AND cr.entry_type = 'CREDIT'
WHERE dr.journal_definition_id = d.id
  AND dr.line_seq = 1
  AND dr.entry_type = 'DEBIT';

ALTER TABLE fin_journal_definitions ALTER COLUMN debit_account_code SET NOT NULL;
ALTER TABLE fin_journal_definitions ALTER COLUMN credit_account_code SET NOT NULL;

DROP INDEX IF EXISTS idx_fin_journal_lines_definition;
DROP TABLE IF EXISTS fin_journal_lines;
