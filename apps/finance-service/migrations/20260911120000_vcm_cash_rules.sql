-- +goose Up

-- VCM cash rule cards: quỹ tiền mặt (1011) đối ứng tiền gửi ngân hàng
-- (1131). Trước đây cả 2 chân cùng map 1131 nên bút toán là no-op; 1011 đã
-- có sẵn trong pilot COA seed.

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'VCM_CASH_ACCOUNT', 'V1', '1011', '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

INSERT INTO fin_accounting_rules (
    tenant_id, document_type, line_no, direction, resolution_type,
    acc_classification, required_dimensions, description_template
) VALUES
    ('00000000-0000-0000-0000-000000000010', 'VCM_CASH_IN',  1, 'DEBIT',  'CLASS_MAP', 'VCM_CASH_ACCOUNT',        '{}', 'Thu tiền mặt vào quỹ'),
    ('00000000-0000-0000-0000-000000000010', 'VCM_CASH_IN',  2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{}', 'Thu tiền mặt vào quỹ'),
    ('00000000-0000-0000-0000-000000000010', 'VCM_CASH_OUT', 1, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{}', 'Chi tiền mặt từ quỹ'),
    ('00000000-0000-0000-0000-000000000010', 'VCM_CASH_OUT', 2, 'CREDIT', 'CLASS_MAP', 'VCM_CASH_ACCOUNT',        '{}', 'Chi tiền mặt từ quỹ')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down

DELETE FROM fin_accounting_rules
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND document_type IN ('VCM_CASH_IN', 'VCM_CASH_OUT');
DELETE FROM fin_acc_class_coa_maps
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND classification = 'VCM_CASH_ACCOUNT';
