-- +goose Up

-- IBM (tiền gửi liên ngân hàng) rule cards + class maps — PROVISIONAL: chờ
-- đối chiếu dev_fac (Q12 §8.4). EPAS IBM.200.01/300.01/301.01/302.01/304.01
-- hạch toán qua bpmFacListener với cấu hình nằm trong DB EPAS; tài khoản dưới
-- đây là giả định an toàn (1321 tiền gửi tại TCTD khác, 13101 phải thu lãi,
-- 5111 thu lãi), sửa sau chỉ cần UPDATE card/map.

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'V1', '1321', 'Tiền gửi tại TCTD khác', 'ASSET', 'D')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'IBM_INTERBANK_DEPOSIT',  'V1', '1321',  '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_INTEREST_RECEIVABLE','V1', '13101', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_INTEREST_INCOME',    'V1', '5111',  '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

INSERT INTO fin_accounting_rules (
    tenant_id, document_type, line_no, direction, resolution_type,
    acc_classification, required_dimensions, description_template
) VALUES
    ('00000000-0000-0000-0000-000000000010', 'IBM_PLACE',    1, 'DEBIT',  'CLASS_MAP', 'IBM_INTERBANK_DEPOSIT',   '{counterparty_code}', 'Mở hợp đồng tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_PLACE',    2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Mở hợp đồng tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_TOP_UP',   1, 'DEBIT',  'CLASS_MAP', 'IBM_INTERBANK_DEPOSIT',   '{counterparty_code}', 'Nộp thêm tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_TOP_UP',   2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Nộp thêm tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_INTEREST', 1, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Thu lãi tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_INTEREST', 2, 'CREDIT', 'CLASS_MAP', 'IBM_INTEREST_INCOME',     '{counterparty_code}', 'Thu lãi tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_EXPECTED', 1, 'DEBIT',  'CLASS_MAP', 'IBM_INTEREST_RECEIVABLE', '{counterparty_code}', 'Dự thu lãi tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_EXPECTED', 2, 'CREDIT', 'CLASS_MAP', 'IBM_INTEREST_INCOME',     '{counterparty_code}', 'Dự thu lãi tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_WITHDRAW', 1, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{counterparty_code}', 'Rút tiền gửi liên ngân hàng'),
    ('00000000-0000-0000-0000-000000000010', 'IBM_WITHDRAW', 2, 'CREDIT', 'CLASS_MAP', 'IBM_INTERBANK_DEPOSIT',   '{counterparty_code}', 'Rút tiền gửi liên ngân hàng')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down

DELETE FROM fin_accounting_rules
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND document_type IN ('IBM_PLACE', 'IBM_TOP_UP', 'IBM_INTEREST', 'IBM_EXPECTED', 'IBM_WITHDRAW');
DELETE FROM fin_acc_class_coa_maps
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND classification IN ('IBM_INTERBANK_DEPOSIT', 'IBM_INTEREST_RECEIVABLE', 'IBM_INTEREST_INCOME');
