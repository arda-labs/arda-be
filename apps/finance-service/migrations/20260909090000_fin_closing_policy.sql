-- +goose Up

-- Iteration 11 wave 1: closing (kết chuyển thu chi, FAC.203.01) + posting
-- policy/backdate. Three parts:
--   1. acc_purpose on fin_coa_accounts — the INC/EXP flag the closing
--      candidate picker selects on (asset/liability nature of use, NULL
--      otherwise).
--   2. fin_posting_policies — per (tenant, business_doc_type) posting-date
--      policy consumed by PostingService.EnsurePostingDateAllowed.
--   3. fin_accounting_rules closing destinations — the result account
--      (4211) the closing flow nets income/expense into.

-- ── 1. acc_purpose ──────────────────────────────────────────────────────
ALTER TABLE fin_coa_accounts ADD COLUMN IF NOT EXISTS acc_purpose VARCHAR(16);
COMMENT ON COLUMN fin_coa_accounts.acc_purpose IS 'Asset/liability nature of use: INC (income) | EXP (expense) | NULL otherwise';

-- Pilot COA (tenant + V1 mirror 20260907090300_pilot_coa_seed.sql). The
-- pilot has income accounts (5111/5112, INCOME type, acc_nature 'C' — the
-- single-letter seed convention, D = debit-normal / C = credit-normal) but
-- no postable expense accounts, so the minimal closing surface is seeded:
-- 7111 income + 6321/8111 expenses, same insert shape as the off-balance
-- seed (20260908160000).
INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature, is_postable, effective_date)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'V1', '7111', 'Doanh thu cung cap dich vu', 'INCOME',  'C', TRUE, '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '6321', 'Gia von hang ban',            'EXPENSE', 'D', TRUE, '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '8111', 'Chi phi kinh doanh khac',     'EXPENSE', 'D', TRUE, '2026-01-01')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

-- Idempotent purpose stamps (also cover rows that pre-existed the insert).
UPDATE fin_coa_accounts SET acc_purpose = 'INC'
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND version_code = 'V1'
  AND acc_code IN ('5111', '5112', '7111');
UPDATE fin_coa_accounts SET acc_purpose = 'EXP'
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND version_code = 'V1'
  AND acc_code IN ('6321', '8111');

-- Result account 4211: every closing nets income (CREDIT) and expense
-- (DEBIT) into it, so a fixed D/C nature would fail the Reserve
-- availability check on one side of a fresh closing. acc_nature 'B' is the
-- codebase's availability-exempt convention (mirror the off-balance memo
-- accounts) — the result account must aggregate both sides unconditionally.
INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature, is_postable, effective_date)
VALUES ('00000000-0000-0000-0000-000000000010', 'V1', '4211', 'Ket qua kinh doanh', 'EQUITY', 'B', TRUE, '2026-01-01')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

-- ── 2. Posting policy / backdate ────────────────────────────────────────
-- tenant_id stays VARCHAR(64) (the fin_* convention) — the pilot seeds carry
-- the iam default tenant as an opaque string, not a native UUID.
CREATE TABLE IF NOT EXISTS fin_posting_policies (
    tenant_id          VARCHAR(64) NOT NULL,
    business_doc_type  VARCHAR(64) NOT NULL,
    allow_backdate     BOOLEAN NOT NULL DEFAULT FALSE,
    max_backdate_days  INT NOT NULL DEFAULT 0,
    check_closing_lock BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, business_doc_type)
);

-- No row for a doc type = no policy constraints (EPAS semantics: the check
-- passes). Seeded rows: all finance case-flow doc types allow 90-day
-- backdating and respect the closing lock; the closing flow itself gets a
-- tighter 30-day window.
INSERT INTO fin_posting_policies (tenant_id, business_doc_type, allow_backdate, max_backdate_days, check_closing_lock)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'FIN_SINGLE_ENTRY', TRUE, 90, TRUE),
  ('00000000-0000-0000-0000-000000000010', 'FIN_DOUBLE_ENTRY', TRUE, 90, TRUE),
  ('00000000-0000-0000-0000-000000000010', 'FIN_OFF_BALANCE',  TRUE, 90, TRUE),
  ('00000000-0000-0000-0000-000000000010', 'FIN_TXN_CANCEL',   TRUE, 90, TRUE),
  ('00000000-0000-0000-0000-000000000010', 'FIN_CLOSING',      TRUE, 30, TRUE)
ON CONFLICT (tenant_id, business_doc_type) DO NOTHING;

-- ── 3. Closing destinations ─────────────────────────────────────────────
-- No schema change: fin_accounting_rules already carries the FIXED_CODE
-- resolution type with an account_ref column. The rule keys ride the
-- document_type column (config-key pattern, same as the class maps).
INSERT INTO fin_accounting_rules (tenant_id, document_type, line_no, direction, resolution_type, account_ref, description_template)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'FIN_CLOSING_INC_DEST', 1, 'CREDIT', 'FIXED_CODE', '4211', 'Ket chuyen doanh thu ve TK 4211'),
  ('00000000-0000-0000-0000-000000000010', 'FIN_CLOSING_EXP_DEST', 1, 'DEBIT',  'FIXED_CODE', '4211', 'Ket chuyen chi phi ve TK 4211')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
