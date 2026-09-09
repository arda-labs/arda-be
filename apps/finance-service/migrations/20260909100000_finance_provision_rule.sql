-- +goose Up

-- Iteration 12: seed the LNM_PROVISION rule card the loan provision batch
-- queries via gRPC ListPostingRules (libs PostingLinesFromRules). Mirrors the
-- 20260909092000 wire format. Lines 1-2 are the trích tăng pair (DR expense /
-- CR liability), lines 3-4 the hoàn giảm pair (DR liability / CR release) —
-- the service selects the card line by delta sign. Classifications mirror the
-- strings the provision service previously hardcoded; their COA mappings
-- (61112/22902/71106) were seeded by 20260907130000_provision_accounts.sql.
-- LNM_ACCRUAL is NOT re-seeded here — 20260907090200 already carries its two
-- lines. Idempotent re-seed on conflict.

INSERT INTO fin_accounting_rules (tenant_id, document_type, line_no, direction, resolution_type, acc_classification, required_dimensions, description_template)
VALUES
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION', 1, 'DEBIT',  'CLASS_MAP', 'LNM_PROVISION_EXPENSE',  '{contract_code,debt_group_code,org_unit_code}', 'Trích lập dự phòng (tăng)'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION', 2, 'CREDIT', 'CLASS_MAP', 'LNM_PROVISION_LIABILITY','{contract_code,debt_group_code,org_unit_code}', 'Trích lập dự phòng (tăng)'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION', 3, 'DEBIT',  'CLASS_MAP', 'LNM_PROVISION_LIABILITY','{contract_code,org_unit_code}',                 'Hoàn giảm dự phòng'),
  ('00000000-0000-0000-0000-000000000010', 'LNM_PROVISION', 4, 'CREDIT', 'CLASS_MAP', 'LNM_PROVISION_RELEASE',  '{contract_code,org_unit_code}',                 'Hoàn giảm dự phòng')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
