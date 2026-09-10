-- +goose Up

-- DPM.102/103 product register/edit case types. Roles reuse DPM_MAKER /
-- DPM_CHECKER; task rows key on the BPMN element ids (UT_*).

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('DPM_PRODUCT_REGISTER_V1', 'DEPOSIT', 'Đăng ký sản phẩm tiền gửi (v1)', 'dpm-product-register-v1', 1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('DPM_PRODUCT_EDIT_V1',     'DEPOSIT', 'Điều chỉnh sản phẩm tiền gửi (v1)', 'dpm-product-edit-v1', 1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_DPM_PRODUCT_REGISTER_MAKER',   'DPM_PRODUCT_REGISTER_V1', 'UT_MakerInput',    'DPM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_PRODUCT_REGISTER_CHECKER', 'DPM_PRODUCT_REGISTER_V1', 'UT_CheckerReview', 'DPM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_DPM_PRODUCT_EDIT_MAKER',       'DPM_PRODUCT_EDIT_V1',     'UT_MakerInput',    'DPM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_PRODUCT_EDIT_CHECKER',     'DPM_PRODUCT_EDIT_V1',     'UT_CheckerReview', 'DPM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_DPM_PRODUCT_REGISTER_MAKER',   'DPM_PRODUCT_REGISTER_V1', 'UT_MakerInput',    'Nhập sản phẩm tiền gửi',   'DPM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_DPM_PRODUCT_REGISTER_CHECKER', 'DPM_PRODUCT_REGISTER_V1', 'UT_CheckerReview', 'Phê duyệt sản phẩm tiền gửi', 'DPM_CHECKER', 'OPEN,APPROVE',     'ACTIVE'),
    ('BPR_DPM_PRODUCT_EDIT_MAKER',       'DPM_PRODUCT_EDIT_V1',     'UT_MakerInput',    'Chỉnh sửa sản phẩm tiền gửi', 'DPM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_DPM_PRODUCT_EDIT_CHECKER',     'DPM_PRODUCT_EDIT_V1',     'UT_CheckerReview', 'Phê duyệt sản phẩm tiền gửi', 'DPM_CHECKER', 'OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_DPM_PRODUCT_REGISTER_V1_24H', 'SLA_DPM_PRODUCT_REGISTER_V1_24H', 'Đăng ký sản phẩm 24h', 'DPM_PRODUCT_REGISTER_V1', 24, 4, '', 'ACTIVE'),
    ('SLA_DPM_PRODUCT_EDIT_V1_24H',     'SLA_DPM_PRODUCT_EDIT_V1_24H',     'Điều chỉnh sản phẩm 24h', 'DPM_PRODUCT_EDIT_V1', 24, 4, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_DPM_PRODUCT_REG_MAKER',   'SLA_DPM_PRODUCT_REGISTER_V1_24H', 'UT_MakerInput',    'Nhập sản phẩm tiền gửi',   4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_DPM_PRODUCT_REG_CHECKER', 'SLA_DPM_PRODUCT_REGISTER_V1_24H', 'UT_CheckerReview', 'Phê duyệt sản phẩm tiền gửi', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_DPM_PRODUCT_EDT_MAKER',   'SLA_DPM_PRODUCT_EDIT_V1_24H',     'UT_MakerInput',    'Chỉnh sửa sản phẩm tiền gửi', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_DPM_PRODUCT_EDT_CHECKER', 'SLA_DPM_PRODUCT_EDIT_V1_24H',     'UT_CheckerReview', 'Phê duyệt sản phẩm tiền gửi', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
