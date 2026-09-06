-- +goose Up

-- P1b.4c: provision accounts + class maps (docs/accounting-rule-cards.md
-- LNM_PROVISION — EPAS 61112/22902/71106). Corrects the earlier
-- LNM_PROVISION->1319 placeholder.

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'V1', '61112', 'Trích lập dự phòng rủi ro', 'EXPENSE', 'D'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '22902', 'Dự phòng rủi ro chung', 'ASSET', 'C'),
  ('00000000-0000-0000-0000-000000000010', 'V1', '71106', 'Hoàn lập dự phòng', 'INCOME', 'C')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

UPDATE fin_acc_class_coa_maps SET coa_acc_code = '22902'
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND classification = 'LNM_PROVISION';

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION_EXPENSE',  'V1', '61112', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION_LIABILITY', 'V1', '22902', '2026-01-01'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION_RELEASE',   'V1', '71106', '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

-- +goose Down
SELECT 1;
