-- +goose Up

-- Quỹ (fund) posting setup for the EPAS PLI_B/PLI_C "phụ lục phân phối thu chi"
-- forms: class maps for each fund account (41801-41803, 35302-35305) + the
-- appropriation source (profit 4211) and utilization offset (cash 1131 /
-- other expense 8111), plus maker-usable rule cards. PROVISIONAL: chờ đối
-- chiếu dev_fac; chỉnh bằng UPDATE fin_acc_class_coa_maps / fin_accounting_rules.

INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'FUND_DEV', 'V1', '41801', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_FIN_RESERVE', 'V1', '41802', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_OP_SUPPLEMENT', 'V1', '41803', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_BONUS_MANAGER', 'V1', '35302', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_BONUS_STAFF', 'V1', '35303', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_WELFARE_FIXED', 'V1', '35304', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_WELFARE_BOD', 'V1', '35305', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_PROFIT_SOURCE', 'V1', '4211', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_OWNER_CAPITAL', 'V1', '411', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_CASH', 'V1', '1131', '2026-01-01'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_OTHER_EXPENSE', 'V1', '8111', '2026-01-01')
ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING;

INSERT INTO fin_accounting_rules (
    tenant_id, document_type, line_no, direction, resolution_type,
    acc_classification, required_dimensions, description_template
) VALUES
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_DEV', 1, 'DEBIT',  'CLASS_MAP', 'FUND_PROFIT_SOURCE', '{}', 'Trích lập Quỹ đầu tư phát triển'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_DEV', 2, 'CREDIT', 'CLASS_MAP', 'FUND_DEV', '{}', 'Trích lập Quỹ đầu tư phát triển'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_DEV', 1, 'DEBIT',  'CLASS_MAP', 'FUND_DEV', '{}', 'Sử dụng Quỹ đầu tư phát triển'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_DEV', 2, 'CREDIT', 'CLASS_MAP', 'FUND_CASH', '{}', 'Sử dụng Quỹ đầu tư phát triển'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_FIN_RESERVE', 1, 'DEBIT',  'CLASS_MAP', 'FUND_PROFIT_SOURCE', '{}', 'Trích lập Quỹ dự phòng tài chính'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_FIN_RESERVE', 2, 'CREDIT', 'CLASS_MAP', 'FUND_FIN_RESERVE', '{}', 'Trích lập Quỹ dự phòng tài chính'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_FIN_RESERVE', 1, 'DEBIT',  'CLASS_MAP', 'FUND_FIN_RESERVE', '{}', 'Sử dụng Quỹ dự phòng tài chính'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_FIN_RESERVE', 2, 'CREDIT', 'CLASS_MAP', 'FUND_CASH', '{}', 'Sử dụng Quỹ dự phòng tài chính'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_OP_SUPPLEMENT', 1, 'DEBIT',  'CLASS_MAP', 'FUND_PROFIT_SOURCE', '{}', 'Trích lập Quỹ dự trữ bổ sung vốn hoạt động'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_OP_SUPPLEMENT', 2, 'CREDIT', 'CLASS_MAP', 'FUND_OP_SUPPLEMENT', '{}', 'Trích lập Quỹ dự trữ bổ sung vốn hoạt động'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_OP_SUPPLEMENT', 1, 'DEBIT',  'CLASS_MAP', 'FUND_OP_SUPPLEMENT', '{}', 'Sử dụng Quỹ dự trữ bổ sung vốn hoạt động'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_OP_SUPPLEMENT', 2, 'CREDIT', 'CLASS_MAP', 'FUND_CASH', '{}', 'Sử dụng Quỹ dự trữ bổ sung vốn hoạt động'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_BONUS_MANAGER', 1, 'DEBIT',  'CLASS_MAP', 'FUND_PROFIT_SOURCE', '{}', 'Trích lập Quỹ thưởng cán bộ quản lý'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_BONUS_MANAGER', 2, 'CREDIT', 'CLASS_MAP', 'FUND_BONUS_MANAGER', '{}', 'Trích lập Quỹ thưởng cán bộ quản lý'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_BONUS_MANAGER', 1, 'DEBIT',  'CLASS_MAP', 'FUND_BONUS_MANAGER', '{}', 'Sử dụng Quỹ thưởng cán bộ quản lý'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_BONUS_MANAGER', 2, 'CREDIT', 'CLASS_MAP', 'FUND_CASH', '{}', 'Sử dụng Quỹ thưởng cán bộ quản lý'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_BONUS_STAFF', 1, 'DEBIT',  'CLASS_MAP', 'FUND_PROFIT_SOURCE', '{}', 'Trích lập Quỹ thưởng cho nhân viên'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_BONUS_STAFF', 2, 'CREDIT', 'CLASS_MAP', 'FUND_BONUS_STAFF', '{}', 'Trích lập Quỹ thưởng cho nhân viên'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_BONUS_STAFF', 1, 'DEBIT',  'CLASS_MAP', 'FUND_BONUS_STAFF', '{}', 'Sử dụng Quỹ thưởng cho nhân viên'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_BONUS_STAFF', 2, 'CREDIT', 'CLASS_MAP', 'FUND_CASH', '{}', 'Sử dụng Quỹ thưởng cho nhân viên'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_WELFARE_FIXED', 1, 'DEBIT',  'CLASS_MAP', 'FUND_PROFIT_SOURCE', '{}', 'Trích lập Quỹ phúc lợi hình thành TSCĐ'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_WELFARE_FIXED', 2, 'CREDIT', 'CLASS_MAP', 'FUND_WELFARE_FIXED', '{}', 'Trích lập Quỹ phúc lợi hình thành TSCĐ'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_WELFARE_FIXED', 1, 'DEBIT',  'CLASS_MAP', 'FUND_WELFARE_FIXED', '{}', 'Sử dụng Quỹ phúc lợi hình thành TSCĐ'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_WELFARE_FIXED', 2, 'CREDIT', 'CLASS_MAP', 'FUND_CASH', '{}', 'Sử dụng Quỹ phúc lợi hình thành TSCĐ'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_WELFARE_BOD', 1, 'DEBIT',  'CLASS_MAP', 'FUND_PROFIT_SOURCE', '{}', 'Trích lập Quỹ phúc lợi ban quản lý điều hành'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_APPROP_WELFARE_BOD', 2, 'CREDIT', 'CLASS_MAP', 'FUND_WELFARE_BOD', '{}', 'Trích lập Quỹ phúc lợi ban quản lý điều hành'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_WELFARE_BOD', 1, 'DEBIT',  'CLASS_MAP', 'FUND_WELFARE_BOD', '{}', 'Sử dụng Quỹ phúc lợi ban quản lý điều hành'),
    ('00000000-0000-0000-0000-000000000010', 'FUND_USE_WELFARE_BOD', 2, 'CREDIT', 'CLASS_MAP', 'FUND_CASH', '{}', 'Sử dụng Quỹ phúc lợi ban quản lý điều hành')
ON CONFLICT (tenant_id, document_type, line_no) DO NOTHING;

-- +goose Down

DELETE FROM fin_accounting_rules
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND document_type IN ('FUND_APPROP_DEV', 'FUND_USE_DEV', 'FUND_APPROP_FIN_RESERVE', 'FUND_USE_FIN_RESERVE', 'FUND_APPROP_OP_SUPPLEMENT', 'FUND_USE_OP_SUPPLEMENT', 'FUND_APPROP_BONUS_MANAGER', 'FUND_USE_BONUS_MANAGER', 'FUND_APPROP_BONUS_STAFF', 'FUND_USE_BONUS_STAFF', 'FUND_APPROP_WELFARE_FIXED', 'FUND_USE_WELFARE_FIXED', 'FUND_APPROP_WELFARE_BOD', 'FUND_USE_WELFARE_BOD');
DELETE FROM fin_acc_class_coa_maps
WHERE tenant_id = '00000000-0000-0000-0000-000000000010'
  AND classification IN ('FUND_DEV', 'FUND_FIN_RESERVE', 'FUND_OP_SUPPLEMENT', 'FUND_BONUS_MANAGER', 'FUND_BONUS_STAFF', 'FUND_WELFARE_FIXED', 'FUND_WELFARE_BOD', 'FUND_PROFIT_SOURCE', 'FUND_OWNER_CAPITAL', 'FUND_CASH', 'FUND_OTHER_EXPENSE');
