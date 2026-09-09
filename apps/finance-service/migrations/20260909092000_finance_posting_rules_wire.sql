-- +goose Up

-- Iteration 11 wave 2: wire the fin_accounting_rules cards the workflow
-- workers actually query via gRPC ListPostingRules. The worker document
-- types are LNM_DISB_REGISTER / LNM_DISB_COMPLETE / LNM_COLLECTION (the
-- 20260907090200 seed used LNM_DISBURSEMENT / LNM_DISBURSEMENT_COMPLETE
-- before the rule cards had a caller — those rows stay as historical EPAS
-- reference). Classifications mirror the strings the workers previously
-- hardcoded; idempotent re-seed on conflict.

INSERT INTO fin_accounting_rules (tenant_id, document_type, line_no, direction, resolution_type, acc_classification, required_dimensions, description_template)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISB_REGISTER', 1, 'DEBIT',  'CLASS_MAP', 'LNM_LOAN_PRINCIPAL',           '{contract_code,agreement_code,debt_group_code,customer_code,org_unit_code}', 'Cho vay khách hàng'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISB_REGISTER', 2, 'CREDIT', 'CLASS_MAP', 'FUND_DISBURSEMENT_IN_TRANSIT', '{contract_code,org_unit_code}',                                              'Tiền đang chuyển'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISB_COMPLETE', 1, 'DEBIT',  'CLASS_MAP', 'FUND_DISBURSEMENT_IN_TRANSIT', '{contract_code,org_unit_code}',                                              'Đảo chiều tiền đang chuyển'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISB_COMPLETE', 2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT',      '{contract_code,customer_code,org_unit_code}',                                'Xuất quỹ hoàn tất giải ngân'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_COLLECTION',    1, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT',      '{contract_code,customer_code,org_unit_code}',                                'Thu tiền gốc'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_COLLECTION',    2, 'CREDIT', 'CLASS_MAP', 'LNM_LOAN_PRINCIPAL',           '{contract_code,agreement_code,customer_code,org_unit_code}',                 'Giảm cho vay khách hàng'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_COLLECTION',    3, 'DEBIT',  'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT',      '{contract_code,customer_code,org_unit_code}',                                'Thu tiền lãi'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_COLLECTION',    4, 'CREDIT', 'CLASS_MAP', 'LNM_INTEREST_RECEIVABLE',      '{contract_code,org_unit_code}',                                              'Giảm phải thu lãi cho vay')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
