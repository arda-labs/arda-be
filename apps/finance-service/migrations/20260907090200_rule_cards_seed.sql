-- +goose Up

-- P1a.2: seed dimension registry + accounting rules from EPAS dev_fac
-- evidence (docs/accounting-rule-cards.md). Classifications are config
-- keys — their COA mappings live in fin_acc_class_coa_maps.

INSERT INTO fin_dimension_keys (tenant_id, key, data_type, required_for, description)
VALUES
  (NULL, 'contract_code',   'STRING', '{LNM_DISBURSEMENT,LNM_COLLECTION,LNM_ACCRUAL}', 'Mã hợp đồng tín dụng'),
  (NULL, 'agreement_code',  'STRING', '{LNM_DISBURSEMENT,LNM_COLLECTION}',             'Mã hợp đồng giải ngân'),
  (NULL, 'debt_group_code', 'CODE',   '{LNM_DISBURSEMENT,LNM_ACCRUAL}',                'Nhóm nợ (TT 02/2023)'),
  (NULL, 'customer_code',   'STRING', '{LNM_DISBURSEMENT,LNM_COLLECTION}',             'Mã khách hàng'),
  (NULL, 'org_unit_code',   'CODE',   '{}',                                            'Đơn vị org bắt buộc mọi luồng')
ON CONFLICT (COALESCE(tenant_id,''), key) DO NOTHING;

INSERT INTO fin_accounting_rules (tenant_id, document_type, line_no, direction, resolution_type, acc_classification, required_dimensions, description_template)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISBURSEMENT', 1, 'DEBIT',  'CLASS_MAP', 'LNM_LOAN_PRINCIPAL',            '{contract_code,agreement_code,debt_group_code,customer_code,org_unit_code}', 'Cho vay khách hàng'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISBURSEMENT', 2, 'CREDIT', 'CLASS_MAP', 'FUND_DISBURSEMENT_IN_TRANSIT',  '{contract_code,org_unit_code}',                                              'Tiền đang chuyển'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_COLLECTION',   1, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT',       '{contract_code,customer_code,org_unit_code}',                                'Thu tiền'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_COLLECTION',   2, 'CREDIT', 'CLASS_MAP', 'LNM_LOAN_PRINCIPAL',            '{contract_code,agreement_code,customer_code,org_unit_code}',                 'Giảm cho vay khách hàng'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_ACCRUAL',      1, 'DEBIT',  'CLASS_MAP', 'LNM_INTEREST_RECEIVABLE',       '{contract_code,debt_group_code,org_unit_code}',                              'Phải thu lãi cho vay'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_ACCRUAL',      2, 'CREDIT', 'CLASS_MAP', 'LNM_INTEREST_INCOME',           '{contract_code,org_unit_code}',                                              'Doanh thu lãi cho vay')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
