-- +goose Up

-- IBM (tiền gửi liên ngân hàng) case types: placement + 4 movement kinds.
-- Maker submits the staged object in deposit-service; checker APPROVE posts
-- via provisional IBM_* rule cards.

INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('IBM_MAKER',   'IBM maker - lập giao dịch liên ngân hàng',  'MAKER',   'IBM', 'ACTIVE'),
    ('IBM_CHECKER', 'IBM checker - duyệt giao dịch liên ngân hàng', 'CHECKER', 'IBM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('IBM_PLACE_V1',    'DEPOSIT', 'Mở hợp đồng tiền gửi liên ngân hàng (v1)', 'ibm-place-v1',    1, TRUE, 'IBM_MAKER', 'IBM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('IBM_TOP_UP_V1',   'DEPOSIT', 'Nộp thêm tiền gửi liên ngân hàng (v1)',    'ibm-movement-v1', 1, TRUE, 'IBM_MAKER', 'IBM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('IBM_INTEREST_V1', 'DEPOSIT', 'Thu lãi tiền gửi liên ngân hàng (v1)',     'ibm-movement-v1', 1, TRUE, 'IBM_MAKER', 'IBM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('IBM_EXPECTED_V1', 'DEPOSIT', 'Dự thu lãi tiền gửi liên ngân hàng (v1)',  'ibm-movement-v1', 1, TRUE, 'IBM_MAKER', 'IBM_CHECKER', 'deposit-service', 'ACTIVE'),
    ('IBM_WITHDRAW_V1', 'DEPOSIT', 'Rút tiền gửi liên ngân hàng (v1)',         'ibm-movement-v1', 1, TRUE, 'IBM_MAKER', 'IBM_CHECKER', 'deposit-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_IBM_PLACE_MAKER',      'IBM_PLACE_V1',    'UT_MakerInput',    'IBM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_IBM_PLACE_CHECKER',    'IBM_PLACE_V1',    'UT_CheckerReview', 'IBM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_IBM_TOP_UP_MAKER',     'IBM_TOP_UP_V1',   'UT_MakerInput',    'IBM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_IBM_TOP_UP_CHECKER',   'IBM_TOP_UP_V1',   'UT_CheckerReview', 'IBM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_IBM_INTEREST_MAKER',   'IBM_INTEREST_V1', 'UT_MakerInput',    'IBM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_IBM_INTEREST_CHECKER', 'IBM_INTEREST_V1', 'UT_CheckerReview', 'IBM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_IBM_EXPECTED_MAKER',   'IBM_EXPECTED_V1', 'UT_MakerInput',    'IBM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_IBM_EXPECTED_CHECKER', 'IBM_EXPECTED_V1', 'UT_CheckerReview', 'IBM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_IBM_WITHDRAW_MAKER',   'IBM_WITHDRAW_V1', 'UT_MakerInput',    'IBM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_IBM_WITHDRAW_CHECKER', 'IBM_WITHDRAW_V1', 'UT_CheckerReview', 'IBM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_IBM_PLACE_MAKER',      'IBM_PLACE_V1',    'UT_MakerInput',    'Lập hợp đồng liên ngân hàng',   'IBM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_IBM_PLACE_CHECKER',    'IBM_PLACE_V1',    'UT_CheckerReview', 'Phê duyệt hợp đồng liên ngân hàng', 'IBM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_IBM_TOP_UP_MAKER',     'IBM_TOP_UP_V1',   'UT_MakerInput',    'Lập nộp thêm liên ngân hàng',   'IBM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_IBM_TOP_UP_CHECKER',   'IBM_TOP_UP_V1',   'UT_CheckerReview', 'Phê duyệt nộp thêm liên ngân hàng', 'IBM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_IBM_INTEREST_MAKER',   'IBM_INTEREST_V1', 'UT_MakerInput',    'Lập thu lãi liên ngân hàng',    'IBM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_IBM_INTEREST_CHECKER', 'IBM_INTEREST_V1', 'UT_CheckerReview', 'Phê duyệt thu lãi liên ngân hàng', 'IBM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_IBM_EXPECTED_MAKER',   'IBM_EXPECTED_V1', 'UT_MakerInput',    'Lập dự thu lãi liên ngân hàng', 'IBM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_IBM_EXPECTED_CHECKER', 'IBM_EXPECTED_V1', 'UT_CheckerReview', 'Phê duyệt dự thu lãi liên ngân hàng', 'IBM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_IBM_WITHDRAW_MAKER',   'IBM_WITHDRAW_V1', 'UT_MakerInput',    'Lập rút tiền liên ngân hàng',   'IBM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_IBM_WITHDRAW_CHECKER', 'IBM_WITHDRAW_V1', 'UT_CheckerReview', 'Phê duyệt rút tiền liên ngân hàng', 'IBM_CHECKER', 'OPEN,APPROVE', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_IBM_PLACE_V1_24H',    'SLA_IBM_PLACE_V1_24H',    'Hợp đồng liên ngân hàng 24h',  'IBM_PLACE_V1',    24, 4, '', 'ACTIVE'),
    ('SLA_IBM_TOP_UP_V1_24H',   'SLA_IBM_TOP_UP_V1_24H',   'Nộp thêm liên ngân hàng 24h',  'IBM_TOP_UP_V1',   24, 4, '', 'ACTIVE'),
    ('SLA_IBM_INTEREST_V1_24H', 'SLA_IBM_INTEREST_V1_24H', 'Thu lãi liên ngân hàng 24h',   'IBM_INTEREST_V1', 24, 4, '', 'ACTIVE'),
    ('SLA_IBM_EXPECTED_V1_24H', 'SLA_IBM_EXPECTED_V1_24H', 'Dự thu lãi liên ngân hàng 24h','IBM_EXPECTED_V1', 24, 4, '', 'ACTIVE'),
    ('SLA_IBM_WITHDRAW_V1_24H', 'SLA_IBM_WITHDRAW_V1_24H', 'Rút tiền liên ngân hàng 24h',  'IBM_WITHDRAW_V1', 24, 4, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_IBM_PLACE_MAKER',      'SLA_IBM_PLACE_V1_24H',    'UT_MakerInput',    'Lập hợp đồng liên ngân hàng',   4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_IBM_PLACE_CHECKER',    'SLA_IBM_PLACE_V1_24H',    'UT_CheckerReview', 'Phê duyệt hợp đồng liên ngân hàng', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_IBM_TOP_UP_MAKER',     'SLA_IBM_TOP_UP_V1_24H',   'UT_MakerInput',    'Lập nộp thêm liên ngân hàng',   4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_IBM_TOP_UP_CHECKER',   'SLA_IBM_TOP_UP_V1_24H',   'UT_CheckerReview', 'Phê duyệt nộp thêm liên ngân hàng', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_IBM_INTEREST_MAKER',   'SLA_IBM_INTEREST_V1_24H', 'UT_MakerInput',    'Lập thu lãi liên ngân hàng',    4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_IBM_INTEREST_CHECKER', 'SLA_IBM_INTEREST_V1_24H', 'UT_CheckerReview', 'Phê duyệt thu lãi liên ngân hàng', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_IBM_EXPECTED_MAKER',   'SLA_IBM_EXPECTED_V1_24H', 'UT_MakerInput',    'Lập dự thu lãi liên ngân hàng', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_IBM_EXPECTED_CHECKER', 'SLA_IBM_EXPECTED_V1_24H', 'UT_CheckerReview', 'Phê duyệt dự thu lãi liên ngân hàng', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_IBM_WITHDRAW_MAKER',   'SLA_IBM_WITHDRAW_V1_24H', 'UT_MakerInput',    'Lập rút tiền liên ngân hàng',   4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_IBM_WITHDRAW_CHECKER', 'SLA_IBM_WITHDRAW_V1_24H', 'UT_CheckerReview', 'Phê duyệt rút tiền liên ngân hàng', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down

DELETE FROM business_sla_task_policies WHERE id LIKE 'SLA_IBM_%';
DELETE FROM business_sla_policies WHERE id LIKE 'SLA_IBM_%';
DELETE FROM business_process_roles WHERE id LIKE 'BPR_IBM_%';
DELETE FROM workflow_assignment_rules WHERE id LIKE 'ASSIGN_IBM_%';
DELETE FROM business_operation_types WHERE case_type IN ('IBM_PLACE_V1', 'IBM_TOP_UP_V1', 'IBM_INTEREST_V1', 'IBM_EXPECTED_V1', 'IBM_WITHDRAW_V1');
DELETE FROM workflow_role_catalog WHERE role_code IN ('IBM_MAKER', 'IBM_CHECKER');
