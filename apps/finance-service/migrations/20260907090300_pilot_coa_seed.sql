-- +goose Up

-- P1b.0: real COA for the pilot tenant + classification maps + open period.
-- Tenant 00000000-0000-0000-0000-000000000010 = iam default tenant (backfill).
-- Account codes follow the EPAS fac classes observed in
-- docs/accounting-rule-cards.md (13101 phải thu lãi, 51101 thu lãi, 1131
-- tiền gửi ngân hàng, 1311 cho vay khách hàng).

INSERT INTO fin_coa_versions (tenant_id, code, name, effective_date)
VALUES ('00000000-0000-0000-0000-000000000010', 'V1', 'COA 2026', '2026-01-01')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'V1', '1011', 'Tiền mặt tại quỹ',            'ASSET',   'D'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '1131', 'Tiền gửi ngân hàng',          'ASSET',   'D'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '1311', 'Cho vay khách hàng',          'ASSET',   'D'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '1319', 'Dự phòng phải thu khó đòi',   'ASSET',   'C'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '13101', 'Phải thu lãi cho vay',       'ASSET',   'D'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '5111', 'Doanh thu lãi cho vay',       'INCOME',  'C'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '5112', 'Doanh thu phí cho vay',       'INCOME',  'C')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'LNM_LOAN_PRINCIPAL',           'V1', '1311', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'FUND_DISBURSEMENT_IN_TRANSIT','V1', '1131', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'CASH_SETTLEMENT_ACCOUNT',      'V1', '1131', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_INTEREST_RECEIVABLE',      'V1', '13101', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_INTEREST_INCOME',          'V1', '5111', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_FEE_INCOME',               'V1', '5112', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION',                'V1', '1319', '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

INSERT INTO fin_periods (tenant_id, period_code, start_date, end_date, status)
VALUES ('00000000-0000-0000-0000-000000000010', '2026-09', '2026-09-01', '2026-09-30', 'OPEN')
ON CONFLICT (tenant_id, period_code) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
