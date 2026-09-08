-- +goose Up

-- P3a reporting foundation (docs/epas-survey/fac-statistical-reporting-survey.md §5.2):
-- ① daily trial-balance precompute — the EPAS fac_inf_trial_balance anchor
-- (account × currency × business date, open/increment/close Dr/Cr),
-- rebuilt idempotently per date by the COB step FIN_TRIAL_BALANCE_DAILY;
-- every statement/report reads from here, never re-aggregating the journal.
-- ② statement formula config — the EPAS fac_cfg_acct_formula concept without
-- SQL-as-config: formulas are JSON references to accounts, or sibling rows,
-- evaluated by Go code.

CREATE TABLE IF NOT EXISTS fin_trial_balance_daily (
    tenant_id        VARCHAR(64) NOT NULL,
    business_date    DATE NOT NULL,
    coa_version      TEXT NOT NULL,
    account_code     TEXT NOT NULL,
    currency_code    VARCHAR(3) NOT NULL,
    -- opening = previous day's closing (0 on first day); incremental sums
    -- of POSTED movement on business_date itself; closing = open ± incr.
    open_debit_minor  BIGINT NOT NULL DEFAULT 0,
    open_credit_minor BIGINT NOT NULL DEFAULT 0,
    incr_debit_minor  BIGINT NOT NULL DEFAULT 0,
    incr_credit_minor BIGINT NOT NULL DEFAULT 0,
    close_debit_minor  BIGINT NOT NULL DEFAULT 0,
    close_credit_minor BIGINT NOT NULL DEFAULT 0,
    rebuilt_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by       TEXT NOT NULL DEFAULT 'cob',
    PRIMARY KEY (tenant_id, business_date, coa_version, account_code, currency_code)
);

CREATE INDEX IF NOT EXISTS idx_fin_tbd_date
    ON fin_trial_balance_daily (tenant_id, business_date DESC);
CREATE INDEX IF NOT EXISTS idx_fin_tbd_account
    ON fin_trial_balance_daily (tenant_id, coa_version, account_code);

-- Statement definition: one row per statement line. level drives FE indent,
-- row sign is presentation-only (a sign=-1 row displays its credit-nature
-- net as a positive figure); rows-formula members carry their own
-- accounting-role sign (contra members negate) applied to the member's
-- display value, evaluated by Go code against fin_trial_balance_daily.
CREATE TABLE IF NOT EXISTS fin_statement_formula (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      VARCHAR(64) NOT NULL,
    statement_code TEXT NOT NULL,               -- CDKT | B02 | SO_CAI_TH | ...
    row_code       TEXT NOT NULL,               -- stable key (FE i18n + refs)
    parent_code    TEXT,                        -- grouping row this belongs to
    label          TEXT NOT NULL,
    level          INTEGER NOT NULL DEFAULT 0,  -- 0 = top; FE indent depth
    sort_order     INTEGER NOT NULL,
    sign           INTEGER NOT NULL DEFAULT 1 CHECK (sign IN (1, -1)),
    -- Formula: {"type":"accounts","codes":["1131","1011"]} summing net
    -- balance of listed accounts |
    -- {"type":"rows","members":[{"code":"R1"},{"code":"R2","sign":-1}]}
    -- summing other lines' display values, each with its role sign
    -- (+1 normal, -1 contra) within this total |
    -- {"type":"none"} for pure grouping rows.
    formula        JSONB NOT NULL DEFAULT '{"type":"none"}',
    is_total       BOOLEAN NOT NULL DEFAULT FALSE,
    created_by     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     TEXT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, statement_code, row_code)
);

CREATE INDEX IF NOT EXISTS idx_fin_stmt_formula_stmt
    ON fin_statement_formula (tenant_id, statement_code, sort_order);

-- Pilot seeds: balance sheet (CDKT) + operating results (B02) over the pilot
-- COA V1 (20260907090300). Formula accounts match fin_coa_accounts seed;
-- row refs use row_code values defined here. Ledger nets are debit-positive:
-- credit-nature accounts (1319 dự phòng, 5111/5112 doanh thu) carry negative
-- nets, so their display rows use sign=-1 to render positive amounts;
-- B02 total rows aggregate member display values with role signs
-- (expense enters PROFIT with sign=-1).
INSERT INTO fin_statement_formula
    (tenant_id, statement_code, row_code, parent_code, label, level, sort_order, sign, formula, is_total, created_by)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'ASSETS',  NULL, 'TÀI SẢN', 0, 10,  1, '{"type":"none"}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'CASH',    'ASSETS', 'Tiền mặt tại quỹ', 1, 20, 1, '{"type":"accounts","codes":["1011"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'BANK',    'ASSETS', 'Tiền gửi ngân hàng', 1, 30, 1, '{"type":"accounts","codes":["1131"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'LOANS_GROSS', 'ASSETS', 'Cho vay khách hàng', 1, 40, 1, '{"type":"accounts","codes":["1311"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'INT_RECEIVABLE', 'ASSETS', 'Phải thu lãi cho vay', 1, 50, 1, '{"type":"accounts","codes":["13101"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'PROVISION', 'ASSETS', 'Dự phòng phải thu khó đòi', 1, 60, -1, '{"type":"accounts","codes":["1319"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'ASSETS_TOTAL', 'ASSETS', 'Tổng tài sản', 1, 70, 1, '{"type":"rows","members":[{"code":"CASH"},{"code":"BANK"},{"code":"LOANS_GROSS"},{"code":"INT_RECEIVABLE"},{"code":"PROVISION","sign":-1}]}', true, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'LIABILITIES', NULL, 'NƠI PHẢI TRẢ', 0, 80, 1, '{"type":"none"}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'LIAB_TOTAL', 'LIABILITIES', 'Tổng nợ phải trả', 1, 90, 1, '{"type":"rows","members":[]}', true, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'EQUITY', NULL, 'VỐN CHỦ SỞ HỮU', 0, 100, 1, '{"type":"none"}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'EQUITY_TOTAL', 'EQUITY', 'Tổng vốn chủ sở hữu', 1, 110, 1, '{"type":"rows","members":[]}', true, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'CDKT', 'R_TOTAL', NULL, 'TỔNG CỘNG NGUỒN VỐN', 0, 120, 1, '{"type":"rows","members":[{"code":"LIAB_TOTAL"},{"code":"EQUITY_TOTAL"}]}', true, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'INCOME', NULL, 'DOANH THU', 0, 10, 1, '{"type":"none"}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'INT_INCOME', 'INCOME', 'Doanh thu lãi cho vay', 1, 20, -1, '{"type":"accounts","codes":["5111"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'FEE_INCOME', 'INCOME', 'Doanh thu phí cho vay', 1, 30, -1, '{"type":"accounts","codes":["5112"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'INCOME_TOTAL', 'INCOME', 'Tổng doanh thu', 1, 40, 1, '{"type":"rows","members":[{"code":"INT_INCOME"},{"code":"FEE_INCOME"}]}', true, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'EXPENSE', NULL, 'CHI PHÍ', 0, 50, 1, '{"type":"none"}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'PROVISION_EXP', 'EXPENSE', 'Trích lập dự phòng', 1, 60, -1, '{"type":"accounts","codes":["1319"]}', false, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'EXPENSE_TOTAL', 'EXPENSE', 'Tổng chi phí', 1, 70, 1, '{"type":"rows","members":[{"code":"PROVISION_EXP"}]}', true, 'seed'),
  ('00000000-0000-0000-0000-000000000010', 'B02', 'PROFIT', NULL, 'LỢI NHUẬN TRƯỚC THUẾ', 0, 80, 1, '{"type":"rows","members":[{"code":"INCOME_TOTAL"},{"code":"EXPENSE_TOTAL","sign":-1}]}', true, 'seed')
ON CONFLICT (tenant_id, statement_code, row_code) DO NOTHING;

-- +goose Down
-- No down: rebuild mode — superseding schema, not replaceable.
SELECT 1;
