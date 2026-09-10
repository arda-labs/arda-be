-- +goose Up

-- DPM rate + interest case types (DPM.100/101 rates; 302/303/304 ops).
-- Roles reuse DPM_MAKER/DPM_CHECKER.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('DPM_RATE_REGISTER_V1',   'DEPOSIT', 'Đăng ký lãi suất huy động (v1)', 'dpm-rate-v1',     1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('DPM_RATE_EDIT_V1',       'DEPOSIT', 'Điều chỉnh lãi suất huy động (v1)', 'dpm-rate-v1',  1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('DPM_PAY_INTEREST_V1',    'DEPOSIT', 'Trả lãi tiền gửi (v1)',            'dpm-interest-v1', 1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('DPM_CAPITALIZE_V1',      'DEPOSIT', 'Lãi nhập gốc tiền gửi (v1)',       'dpm-interest-v1', 1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('DPM_BATCH_INTEREST_V1',  'DEPOSIT', 'Trả lãi hàng loạt (v1)',           'dpm-interest-v1', 1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_DPM_RATE_REG_MAKER',    'DPM_RATE_REGISTER_V1',  'UT_MakerInput',    'DPM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_RATE_REG_CHECKER',  'DPM_RATE_REGISTER_V1',  'UT_CheckerReview', 'DPM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_DPM_RATE_EDT_MAKER',    'DPM_RATE_EDIT_V1',      'UT_MakerInput',    'DPM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_RATE_EDT_CHECKER',  'DPM_RATE_EDIT_V1',      'UT_CheckerReview', 'DPM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_DPM_PAYINT_MAKER',      'DPM_PAY_INTEREST_V1',   'UT_MakerInput',    'DPM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_PAYINT_CHECKER',    'DPM_PAY_INTEREST_V1',   'UT_CheckerReview', 'DPM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_DPM_CAP_MAKER',         'DPM_CAPITALIZE_V1',     'UT_MakerInput',    'DPM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_CAP_CHECKER',       'DPM_CAPITALIZE_V1',     'UT_CheckerReview', 'DPM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_DPM_BATCHINT_MAKER',    'DPM_BATCH_INTEREST_V1', 'UT_MakerInput',    'DPM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_BATCHINT_CHECKER',  'DPM_BATCH_INTEREST_V1', 'UT_CheckerReview', 'DPM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_DPM_RATE_REG_MAKER',   'DPM_RATE_REGISTER_V1',  'UT_MakerInput',    'Nhập lãi suất huy động', 'DPM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_DPM_RATE_REG_CHECKER', 'DPM_RATE_REGISTER_V1',  'UT_CheckerReview', 'Phê duyệt lãi suất huy động', 'DPM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_DPM_RATE_EDT_MAKER',   'DPM_RATE_EDIT_V1',      'UT_MakerInput',    'Nhập lãi suất huy động', 'DPM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_DPM_RATE_EDT_CHECKER', 'DPM_RATE_EDIT_V1',      'UT_CheckerReview', 'Phê duyệt lãi suất huy động', 'DPM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_DPM_PAYINT_MAKER',     'DPM_PAY_INTEREST_V1',   'UT_MakerInput',    'Lập trả lãi tiền gửi', 'DPM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_DPM_PAYINT_CHECKER',   'DPM_PAY_INTEREST_V1',   'UT_CheckerReview', 'Phê duyệt trả lãi tiền gửi', 'DPM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_DPM_CAP_MAKER',        'DPM_CAPITALIZE_V1',     'UT_MakerInput',    'Lập lãi nhập gốc', 'DPM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_DPM_CAP_CHECKER',      'DPM_CAPITALIZE_V1',     'UT_CheckerReview', 'Phê duyệt lãi nhập gốc', 'DPM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_DPM_BATCHINT_MAKER',   'DPM_BATCH_INTEREST_V1', 'UT_MakerInput',    'Lập trả lãi hàng loạt', 'DPM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_DPM_BATCHINT_CHECKER', 'DPM_BATCH_INTEREST_V1', 'UT_CheckerReview', 'Phê duyệt trả lãi hàng loạt', 'DPM_CHECKER', 'OPEN,APPROVE', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_DPM_RATE_REGISTER_V1_24H',  'SLA_DPM_RATE_REGISTER_V1_24H',  'Lãi suất huy động 24h',  'DPM_RATE_REGISTER_V1',  24, 4, '', 'ACTIVE'),
    ('SLA_DPM_RATE_EDIT_V1_24H',      'SLA_DPM_RATE_EDIT_V1_24H',      'Lãi suất huy động 24h',  'DPM_RATE_EDIT_V1',      24, 4, '', 'ACTIVE'),
    ('SLA_DPM_PAY_INTEREST_V1_24H',   'SLA_DPM_PAY_INTEREST_V1_24H',   'Trả lãi tiền gửi 24h',   'DPM_PAY_INTEREST_V1',   24, 4, '', 'ACTIVE'),
    ('SLA_DPM_CAPITALIZE_V1_24H',     'SLA_DPM_CAPITALIZE_V1_24H',     'Lãi nhập gốc 24h',       'DPM_CAPITALIZE_V1',     24, 4, '', 'ACTIVE'),
    ('SLA_DPM_BATCH_INTEREST_V1_24H', 'SLA_DPM_BATCH_INTEREST_V1_24H', 'Trả lãi hàng loạt 24h',  'DPM_BATCH_INTEREST_V1', 24, 4, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_DPM_RATE_REG_MAKER',     'SLA_DPM_RATE_REGISTER_V1_24H',  'UT_MakerInput',    'Nhập lãi suất huy động', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_DPM_RATE_REG_CHECKER',   'SLA_DPM_RATE_REGISTER_V1_24H',  'UT_CheckerReview', 'Phê duyệt lãi suất huy động', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_DPM_RATE_EDT_MAKER',     'SLA_DPM_RATE_EDIT_V1_24H',      'UT_MakerInput',    'Nhập lãi suất huy động', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_DPM_RATE_EDT_CHECKER',   'SLA_DPM_RATE_EDIT_V1_24H',      'UT_CheckerReview', 'Phê duyệt lãi suất huy động', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_DPM_PAYINT_MAKER',       'SLA_DPM_PAY_INTEREST_V1_24H',   'UT_MakerInput',    'Lập trả lãi tiền gửi', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_DPM_PAYINT_CHECKER',     'SLA_DPM_PAY_INTEREST_V1_24H',   'UT_CheckerReview', 'Phê duyệt trả lãi tiền gửi', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_DPM_CAP_MAKER',          'SLA_DPM_CAPITALIZE_V1_24H',     'UT_MakerInput',    'Lập lãi nhập gốc', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_DPM_CAP_CHECKER',        'SLA_DPM_CAPITALIZE_V1_24H',     'UT_CheckerReview', 'Phê duyệt lãi nhập gốc', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_DPM_BATCHINT_MAKER',     'SLA_DPM_BATCH_INTEREST_V1_24H', 'UT_MakerInput',    'Lập trả lãi hàng loạt', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_DPM_BATCHINT_CHECKER',   'SLA_DPM_BATCH_INTEREST_V1_24H', 'UT_CheckerReview', 'Phê duyệt trả lãi hàng loạt', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down

DELETE FROM business_sla_task_policies WHERE id LIKE 'SLA_DPM_RATE_%' OR id LIKE 'SLA_DPM_PAYINT_%' OR id LIKE 'SLA_DPM_CAP_%' OR id LIKE 'SLA_DPM_BATCHINT_%';
DELETE FROM business_sla_policies WHERE id LIKE 'SLA_DPM_RATE_%' OR id LIKE 'SLA_DPM_PAY_INTEREST_%' OR id LIKE 'SLA_DPM_CAPITALIZE_%' OR id LIKE 'SLA_DPM_BATCH_INTEREST_%';
DELETE FROM business_process_roles WHERE id LIKE 'BPR_DPM_RATE_%' OR id LIKE 'BPR_DPM_PAYINT_%' OR id LIKE 'BPR_DPM_CAP_%' OR id LIKE 'BPR_DPM_BATCHINT_%';
DELETE FROM workflow_assignment_rules WHERE id LIKE 'ASSIGN_DPM_RATE_%' OR id LIKE 'ASSIGN_DPM_PAYINT_%' OR id LIKE 'ASSIGN_DPM_CAP_%' OR id LIKE 'ASSIGN_DPM_BATCHINT_%';
DELETE FROM business_operation_types WHERE case_type IN ('DPM_RATE_REGISTER_V1', 'DPM_RATE_EDIT_V1', 'DPM_PAY_INTEREST_V1', 'DPM_CAPITALIZE_V1', 'DPM_BATCH_INTEREST_V1');
