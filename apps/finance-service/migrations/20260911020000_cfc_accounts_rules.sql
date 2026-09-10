-- +goose Up

-- CFC (vốn nội bộ) rule cards + class maps — PROVISIONAL: chờ đối chiếu
-- dev_fac (Q12 §8.4). EPAS CFM.300.01 (tiếp nhận vốn) / CFM.302.01 (giải ngân)
-- / CFM.303.01 (tất toán) hạch toán qua bpmFacListener với cấu hình nằm trong
-- DB EPAS (không có trong source); các tài khoản dưới đây là giả định an toàn,
-- sửa sau chỉ cần UPDATE fin_coa_accounts / fin_acc_class_coa_maps /
-- fin_accounting_rules.

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'V1', '4311', 'Vốn nội bộ (nguồn vốn)', 'LIABILITY', 'C'),
    ('00000000-0000-0000-0000-000000000010', 'V1', '8011', 'Chi phí lãi vốn nguồn',  'EXPENSE',   'D')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'FUND_CAPITAL_LIABILITY', 'V1', '4311', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_INTEREST_EXPENSE',   'V1', '8011', '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

INSERT INTO fin_accounting_rules (
    tenant_id, document_type, line_no, direction, resolution_type,
    acc_classification, required_dimensions, description_template
) VALUES
    ('00000000-0000-0000-0000-000000000010', 'CFC_RECEIPT',       1, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Tiếp nhận vốn nguồn'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_RECEIPT',       2, 'CREDIT', 'CLASS_MAP', 'FUND_CAPITAL_LIABILITY',  '{counterparty_code}', 'Tiếp nhận vốn nguồn'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_DISBURSEMENT',  1, 'DEBIT',  'CLASS_MAP', 'FUND_CAPITAL_LIABILITY',  '{counterparty_code}', 'Giải ngân vốn nguồn'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_DISBURSEMENT',  2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Giải ngân vốn nguồn'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_PAYMENT',       1, 'DEBIT',  'CLASS_MAP', 'CFC_INTEREST_EXPENSE',    '{counterparty_code}', 'Thanh toán hợp đồng vốn'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_PAYMENT',       2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Thanh toán hợp đồng vốn'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_SETTLEMENT',    1, 'DEBIT',  'CLASS_MAP', 'FUND_CAPITAL_LIABILITY',  '{counterparty_code}', 'Tất toán hợp đồng vốn'),
    ('00000000-0000-0000-0000-000000000010', 'CFC_SETTLEMENT',    2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Tất toán hợp đồng vốn')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down

DELETE FROM fin_accounting_rules
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND document_type IN ('CFC_RECEIPT', 'CFC_DISBURSEMENT', 'CFC_PAYMENT', 'CFC_SETTLEMENT');
DELETE FROM fin_acc_class_coa_maps
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND classification IN ('FUND_CAPITAL_LIABILITY', 'CFC_INTEREST_EXPENSE');
