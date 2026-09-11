-- +goose Up

-- LNM.306 specific provision (per-loan, maker/checker) — provisional rule
-- card chờ đối chiếu dev_fac (Q12 §8.4). Reuses the 307 account set
-- (61112/22902/71106) with a separate document type so the flows can be
-- distinguished in the journal.

INSERT INTO fin_accounting_rules (
    tenant_id, document_type, line_no, direction, resolution_type,
    acc_classification, required_dimensions, description_template
) VALUES
    ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION_306', 1, 'DEBIT',  'CLASS_MAP', 'LNM_PROVISION_EXPENSE',   '{contract_code,debt_group_code,org_unit_code}', 'Trích lập dự phòng cụ thể 306'),
    ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION_306', 2, 'CREDIT', 'CLASS_MAP', 'LNM_PROVISION_LIABILITY', '{contract_code,debt_group_code,org_unit_code}', 'Trích lập dự phòng cụ thể 306'),
    ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION_306', 3, 'DEBIT',  'CLASS_MAP', 'LNM_PROVISION_LIABILITY', '{contract_code,org_unit_code}',                 'Hoàn giảm dự phòng cụ thể 306'),
    ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION_306', 4, 'CREDIT', 'CLASS_MAP', 'LNM_PROVISION_RELEASE',   '{contract_code,org_unit_code}',                 'Hoàn giảm dự phòng cụ thể 306')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down

DELETE FROM fin_accounting_rules
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND document_type = 'LNM_PROVISION_306';
