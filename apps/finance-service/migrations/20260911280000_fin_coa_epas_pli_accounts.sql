-- +goose Up

-- EPAS circular-form accounts (2026-09-11): the PLI_B/PLI_C (phụ lục phân phối
-- thu chi) and PLIIb/PLIIc (tình hình hoạt động TT92) formulas ported from
-- EPAS `fac_cfg_acct_formula` read this microfinance/VFU chart (121 cho vay,
-- 353/411/418 quỹ & vốn CSH, 511/515/711 thu, 611/615/642/811/821 chi). The
-- pilot COA V1 is a bank-style chart, so the referenced codes are added here
-- (headers non-postable so prefix sums do not double-count Arda's leaf
-- accounts that share a prefix, e.g. 511 vs 5111/5112, 611 vs 61112).

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature, parent_code, is_postable)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'V1', '121',    'Cho vay (TT92)',                          'ASSET',   'D', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '35302',  'Quỹ thưởng cán bộ quản lý',               'EQUITY',  'C', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '35303',  'Quỹ thưởng cho nhân viên',                'EQUITY',  'C', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '35304',  'Quỹ phúc lợi hình thành TSCĐ',            'EQUITY',  'C', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '35305',  'Quỹ phúc lợi ban quản lý điều hành',      'EQUITY',  'C', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '411',    'Vốn đầu tư của chủ sở hữu',               'EQUITY',  'C', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '418',    'Các Quỹ thuộc vốn chủ sở hữu',            'EQUITY',  'C', NULL,  false),
  ('00000000-0000-0000-0000-000000000010', 'V1', '41801',  'Quỹ đầu tư phát triển',                   'EQUITY',  'C', '418', true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '41802',  'Quỹ dự phòng tài chính',                  'EQUITY',  'C', '418', true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '41803',  'Quỹ dự trữ bổ sung vốn hoạt động',        'EQUITY',  'C', '418', true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '511',    'Doanh thu hoạt động (TT92)',              'INCOME',  'C', NULL,  false),
  ('00000000-0000-0000-0000-000000000010', 'V1', '515',    'Doanh thu hoạt động tài chính',           'INCOME',  'C', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '711',    'Thu nhập khác (TT92)',                    'INCOME',  'C', NULL,  false),
  ('00000000-0000-0000-0000-000000000010', 'V1', '611',    'Chi phí hoạt động (TT92)',                'EXPENSE', 'D', NULL,  false),
  ('00000000-0000-0000-0000-000000000010', 'V1', '615',    'Chi phí tài chính',                       'EXPENSE', 'D', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '642',    'Chi phí quản lý, kinh doanh',             'EXPENSE', 'D', NULL,  true),
  ('00000000-0000-0000-0000-000000000010', 'V1', '811',    'Chi phí khác (TT92)',                     'EXPENSE', 'D', NULL,  false),
  ('00000000-0000-0000-0000-000000000010', 'V1', '821',    'Chi phí thuế thu nhập doanh nghiệp',      'EXPENSE', 'D', NULL,  true)
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

-- +goose Down

DELETE FROM fin_coa_accounts
WHERE tenant_id = '00000000-0000-0000-0000-000000000010' AND version_code = 'V1'
  AND acc_code IN ('121','35302','35303','35304','35305','411','418','41801','41802','41803','511','515','711','611','615','642','811','821');
