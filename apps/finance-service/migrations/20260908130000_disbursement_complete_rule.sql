-- +goose Up

-- P1b v2 / Wave 3: accounting rule for the COMPLETE leg of the two-flow
-- disbursement (EPAS LNM.300.02 seq 2 — hoàn tất giải ngân). The REGISTER leg
-- keeps the existing LNM_DISBURSEMENT rule (see 20260907090200_rule_cards_seed);
-- this card reverses the in-transit leg into cash. Dimensions follow the
-- CASH_SETTLEMENT_ACCOUNT convention of LNM_COLLECTION line 1.

INSERT INTO fin_accounting_rules (tenant_id, document_type, line_no, direction, resolution_type, acc_classification, required_dimensions, description_template)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISBURSEMENT_COMPLETE', 1, 'DEBIT',  'CLASS_MAP', 'FUND_DISBURSEMENT_IN_TRANSIT', '{contract_code,org_unit_code}',            'Đảo chiều tiền đang chuyển'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_DISBURSEMENT_COMPLETE', 2, 'CREDIT', 'CLASS_MAP', 'CASH_SETTLEMENT_ACCOUNT',      '{contract_code,customer_code,org_unit_code}', 'Xuất quỹ hoàn tất giải ngân')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
