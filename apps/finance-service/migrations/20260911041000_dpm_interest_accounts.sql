-- +goose Up

-- DPM interest accounts + cards (W3) — PROVISIONAL: chờ đối chiếu dev_fac.
-- EPAS DPM.302 trả lãi / 303 lãi nhập gốc / 304 trả lãi hàng loạt / 305 dự chi.

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'V1', '8021', 'Chi phí lãi tiền gửi', 'EXPENSE',   'D'),
    ('00000000-0000-0000-0000-000000000010', 'V1', '4911', 'Lãi phải trả tiền gửi', 'LIABILITY', 'C')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'DPM_INTEREST_EXPENSE', 'V1', '8021', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_INTEREST_PAYABLE', 'V1', '4911', '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

INSERT INTO fin_accounting_rules (
    tenant_id, document_type, line_no, direction, resolution_type,
    acc_classification, required_dimensions, description_template
) VALUES
    ('00000000-0000-0000-0000-000000000010', 'DPM_ACCRUAL',        1, 'DEBIT',  'CLASS_MAP', 'DPM_INTEREST_EXPENSE',   '{customer_code}', 'Dự chi lãi tiền gửi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_ACCRUAL',        2, 'CREDIT', 'CLASS_MAP', 'DPM_INTEREST_PAYABLE',   '{customer_code}', 'Dự chi lãi tiền gửi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_INTEREST_PAY',   1, 'DEBIT',  'CLASS_MAP', 'DPM_INTEREST_PAYABLE',   '{customer_code}', 'Trả lãi tiền gửi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_INTEREST_PAY',   2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{customer_code}', 'Trả lãi tiền gửi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_CAPITALIZE',     1, 'DEBIT',  'CLASS_MAP', 'DPM_INTEREST_PAYABLE',   '{customer_code}', 'Lãi nhập gốc tiền gửi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_CAPITALIZE',     2, 'CREDIT', 'CLASS_MAP', 'DPM_DEPOSIT_LIABILITY',  '{customer_code}', 'Lãi nhập gốc tiền gửi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_SETTLEMENT_V3',  1, 'DEBIT',  'CLASS_MAP', 'DPM_DEPOSIT_LIABILITY',  '{customer_code}', 'Tất toán gốc + lãi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_SETTLEMENT_V3',  2, 'DEBIT',  'CLASS_MAP', 'DPM_INTEREST_PAYABLE',   '{customer_code}', 'Tất toán gốc + lãi'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_SETTLEMENT_V3',  3, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{customer_code}', 'Tất toán gốc + lãi')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down

DELETE FROM fin_accounting_rules
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND document_type IN ('DPM_ACCRUAL', 'DPM_INTEREST_PAY', 'DPM_CAPITALIZE', 'DPM_SETTLEMENT_V3');
DELETE FROM fin_acc_class_coa_maps
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND classification IN ('DPM_INTEREST_EXPENSE', 'DPM_INTEREST_PAYABLE');
