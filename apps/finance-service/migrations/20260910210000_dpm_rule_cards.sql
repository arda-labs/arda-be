-- +goose Up

-- DPM rule cards + class maps (evidence: docs/accounting-rule-cards.md —
-- EPAS DPM.300.01 open / DPM.306.01 settlement, class 423x "Tiền gửi tiết
-- kiệm"). These rows previously existed only in the pilot database, so a
-- freshly built environment resolved neither the rule cards nor the
-- DPM_DEPOSIT_LIABILITY classification and deposit open/settle failed.

INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'V1', '4231', 'Tiền gửi tiết kiệm', 'LIABILITY', 'C')
ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING;

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'DPM_DEPOSIT_LIABILITY', 'V1', '4231', '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

INSERT INTO fin_accounting_rules (
    tenant_id, document_type, line_no, direction, resolution_type,
    acc_classification, required_dimensions, description_template
) VALUES
    ('00000000-0000-0000-0000-000000000010', 'DPM_OPEN',       1, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{customer_code}', 'Nộp tiền gửi tiết kiệm'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_OPEN',       2, 'CREDIT', 'CLASS_MAP', 'DPM_DEPOSIT_LIABILITY',   '{customer_code}', 'Nộp tiền gửi tiết kiệm'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_SETTLEMENT', 1, 'DEBIT',  'CLASS_MAP', 'DPM_DEPOSIT_LIABILITY',   '{customer_code}', 'Tất toán sổ tiết kiệm'),
    ('00000000-0000-0000-0000-000000000010', 'DPM_SETTLEMENT', 2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT', '{customer_code}', 'Tất toán sổ tiết kiệm')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down
DELETE FROM fin_accounting_rules
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND document_type IN ('DPM_OPEN', 'DPM_SETTLEMENT');
DELETE FROM fin_acc_class_coa_maps
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND classification = 'DPM_DEPOSIT_LIABILITY';
