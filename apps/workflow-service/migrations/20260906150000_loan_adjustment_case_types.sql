-- +goose Up

-- Case-types + assignment config for the 10 uniform loan adjustment flows
-- (lnm-<kind>-v2 BPMN, workers lnm.<kind>.*). Generated from the shared kind
-- list — adding a flow regenerates this file.

INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('LNM_CHECKER', 'Credit checker - phê duyệt điều chỉnh', 'CHECKER', 'LNM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('LNM_DEBT_CHANGE_V2', 'CREDIT', 'Chuyển nhóm nợ (v2)', 'lnm-debt-change-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_RATE_CHANGE_V2', 'CREDIT', 'Thay đổi lãi suất (v2)', 'lnm-rate-change-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_RESTRUCTURE_V2', 'CREDIT', 'Gia hạn nợ (v2)', 'lnm-restructure-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_WAIVER_V2', 'CREDIT', 'Miễn giảm lãi (v2)', 'lnm-waiver-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_WRITEOFF_V2', 'CREDIT', 'Xử lý nợ (v2)', 'lnm-writeoff-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_RECOVERY_V2', 'CREDIT', 'Thu hồi nợ (v2)', 'lnm-recovery-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_FUND_CHECK_V2', 'CREDIT', 'Kiểm tra sử dụng vốn (v2)', 'lnm-fund-check-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_REVENUE_ALLOCATION_V2', 'CREDIT', 'Phân bổ doanh thu (v2)', 'lnm-revenue-allocation-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_VFU_FEE_ALLOCATION_V2', 'CREDIT', 'Trích phí ủy thác (v2)', 'lnm-vfu-fee-allocation-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_OFF_BALANCE_EXPORT_V2', 'CREDIT', 'Xuất toán ngoại bảng (v2)', 'lnm-off-balance-export-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_LNM_DEBT_CHANGE_MAKER',  'LNM_DEBT_CHANGE_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_DEBT_CHANGE_CHECKER','LNM_DEBT_CHANGE_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_RATE_CHANGE_MAKER',  'LNM_RATE_CHANGE_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_RATE_CHANGE_CHECKER','LNM_RATE_CHANGE_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_RESTRUCTURE_MAKER',  'LNM_RESTRUCTURE_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_RESTRUCTURE_CHECKER','LNM_RESTRUCTURE_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_WAIVER_MAKER',  'LNM_WAIVER_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_WAIVER_CHECKER','LNM_WAIVER_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_WRITEOFF_MAKER',  'LNM_WRITEOFF_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_WRITEOFF_CHECKER','LNM_WRITEOFF_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_RECOVERY_MAKER',  'LNM_RECOVERY_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_RECOVERY_CHECKER','LNM_RECOVERY_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_FUND_CHECK_MAKER',  'LNM_FUND_CHECK_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_FUND_CHECK_CHECKER','LNM_FUND_CHECK_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_REVENUE_ALLOCATION_MAKER',  'LNM_REVENUE_ALLOCATION_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_REVENUE_ALLOCATION_CHECKER','LNM_REVENUE_ALLOCATION_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_VFU_FEE_ALLOCATION_MAKER',  'LNM_VFU_FEE_ALLOCATION_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_VFU_FEE_ALLOCATION_CHECKER','LNM_VFU_FEE_ALLOCATION_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_OFF_BALANCE_EXPORT_MAKER',  'LNM_OFF_BALANCE_EXPORT_V2', 'maker_input',   'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_OFF_BALANCE_EXPORT_CHECKER','LNM_OFF_BALANCE_EXPORT_V2', 'checker_review','LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_LNM_DEBT_CHANGE_MAKER',   'LNM_DEBT_CHANGE_V2', 'maker_input',   'Nhập hồ sơ chuyển nhóm nợ', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_DEBT_CHANGE_CHECKER', 'LNM_DEBT_CHANGE_V2', 'checker_review','Phê duyệt chuyển nhóm nợ', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_RATE_CHANGE_MAKER',   'LNM_RATE_CHANGE_V2', 'maker_input',   'Nhập hồ sơ thay đổi lãi suất', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_RATE_CHANGE_CHECKER', 'LNM_RATE_CHANGE_V2', 'checker_review','Phê duyệt thay đổi lãi suất', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_RESTRUCTURE_MAKER',   'LNM_RESTRUCTURE_V2', 'maker_input',   'Nhập hồ sơ gia hạn nợ', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_RESTRUCTURE_CHECKER', 'LNM_RESTRUCTURE_V2', 'checker_review','Phê duyệt gia hạn nợ', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_WAIVER_MAKER',   'LNM_WAIVER_V2', 'maker_input',   'Nhập hồ sơ miễn giảm lãi', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_WAIVER_CHECKER', 'LNM_WAIVER_V2', 'checker_review','Phê duyệt miễn giảm lãi', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_WRITEOFF_MAKER',   'LNM_WRITEOFF_V2', 'maker_input',   'Nhập hồ sơ xử lý nợ', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_WRITEOFF_CHECKER', 'LNM_WRITEOFF_V2', 'checker_review','Phê duyệt xử lý nợ', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_RECOVERY_MAKER',   'LNM_RECOVERY_V2', 'maker_input',   'Nhập hồ sơ thu hồi nợ', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_RECOVERY_CHECKER', 'LNM_RECOVERY_V2', 'checker_review','Phê duyệt thu hồi nợ', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_FUND_CHECK_MAKER',   'LNM_FUND_CHECK_V2', 'maker_input',   'Nhập hồ sơ kiểm tra sử dụng vốn', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_FUND_CHECK_CHECKER', 'LNM_FUND_CHECK_V2', 'checker_review','Phê duyệt kiểm tra sử dụng vốn', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_REVENUE_ALLOCATION_MAKER',   'LNM_REVENUE_ALLOCATION_V2', 'maker_input',   'Nhập hồ sơ phân bổ doanh thu', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_REVENUE_ALLOCATION_CHECKER', 'LNM_REVENUE_ALLOCATION_V2', 'checker_review','Phê duyệt phân bổ doanh thu', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_VFU_FEE_ALLOCATION_MAKER',   'LNM_VFU_FEE_ALLOCATION_V2', 'maker_input',   'Nhập hồ sơ trích phí ủy thác', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_VFU_FEE_ALLOCATION_CHECKER', 'LNM_VFU_FEE_ALLOCATION_V2', 'checker_review','Phê duyệt trích phí ủy thác', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_OFF_BALANCE_EXPORT_MAKER',   'LNM_OFF_BALANCE_EXPORT_V2', 'maker_input',   'Nhập hồ sơ xuất toán ngoại bảng', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_OFF_BALANCE_EXPORT_CHECKER', 'LNM_OFF_BALANCE_EXPORT_V2', 'checker_review','Phê duyệt xuất toán ngoại bảng', 'LNM_CHECKER', 'OPEN,APPROVE', 'ACTIVE')
ON CONFLICT (case_type, step_code, iam_role) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_LNM_DEBT_CHANGE_V2_48H', 'SLA_LNM_DEBT_CHANGE_V2_48H', 'Chuyển nhóm nợ 48h', 'LNM_DEBT_CHANGE_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_RATE_CHANGE_V2_48H', 'SLA_LNM_RATE_CHANGE_V2_48H', 'Thay đổi lãi suất 48h', 'LNM_RATE_CHANGE_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_RESTRUCTURE_V2_48H', 'SLA_LNM_RESTRUCTURE_V2_48H', 'Gia hạn nợ 48h', 'LNM_RESTRUCTURE_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_WAIVER_V2_48H', 'SLA_LNM_WAIVER_V2_48H', 'Miễn giảm lãi 48h', 'LNM_WAIVER_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_WRITEOFF_V2_48H', 'SLA_LNM_WRITEOFF_V2_48H', 'Xử lý nợ 48h', 'LNM_WRITEOFF_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_RECOVERY_V2_48H', 'SLA_LNM_RECOVERY_V2_48H', 'Thu hồi nợ 48h', 'LNM_RECOVERY_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_FUND_CHECK_V2_48H', 'SLA_LNM_FUND_CHECK_V2_48H', 'Kiểm tra sử dụng vốn 48h', 'LNM_FUND_CHECK_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_REVENUE_ALLOCATION_V2_48H', 'SLA_LNM_REVENUE_ALLOCATION_V2_48H', 'Phân bổ doanh thu 48h', 'LNM_REVENUE_ALLOCATION_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_VFU_FEE_ALLOCATION_V2_48H', 'SLA_LNM_VFU_FEE_ALLOCATION_V2_48H', 'Trích phí ủy thác 48h', 'LNM_VFU_FEE_ALLOCATION_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_OFF_BALANCE_EXPORT_V2_48H', 'SLA_LNM_OFF_BALANCE_EXPORT_V2_48H', 'Xuất toán ngoại bảng 48h', 'LNM_OFF_BALANCE_EXPORT_V2', 48, 8, 'LNM_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_TASK_LNM_DEBT_CHANGE_INPUT',   'SLA_LNM_DEBT_CHANGE_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_DEBT_CHANGE_APPROVE', 'SLA_LNM_DEBT_CHANGE_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_RATE_CHANGE_INPUT',   'SLA_LNM_RATE_CHANGE_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_RATE_CHANGE_APPROVE', 'SLA_LNM_RATE_CHANGE_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_RESTRUCTURE_INPUT',   'SLA_LNM_RESTRUCTURE_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_RESTRUCTURE_APPROVE', 'SLA_LNM_RESTRUCTURE_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_WAIVER_INPUT',   'SLA_LNM_WAIVER_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_WAIVER_APPROVE', 'SLA_LNM_WAIVER_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_WRITEOFF_INPUT',   'SLA_LNM_WRITEOFF_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_WRITEOFF_APPROVE', 'SLA_LNM_WRITEOFF_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_RECOVERY_INPUT',   'SLA_LNM_RECOVERY_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_RECOVERY_APPROVE', 'SLA_LNM_RECOVERY_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_FUND_CHECK_INPUT',   'SLA_LNM_FUND_CHECK_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_FUND_CHECK_APPROVE', 'SLA_LNM_FUND_CHECK_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_REVENUE_ALLOCATION_INPUT',   'SLA_LNM_REVENUE_ALLOCATION_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_REVENUE_ALLOCATION_APPROVE', 'SLA_LNM_REVENUE_ALLOCATION_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_VFU_FEE_ALLOCATION_INPUT',   'SLA_LNM_VFU_FEE_ALLOCATION_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_VFU_FEE_ALLOCATION_APPROVE', 'SLA_LNM_VFU_FEE_ALLOCATION_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_OFF_BALANCE_EXPORT_INPUT',   'SLA_LNM_OFF_BALANCE_EXPORT_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_OFF_BALANCE_EXPORT_APPROVE', 'SLA_LNM_OFF_BALANCE_EXPORT_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

UPDATE business_operation_types bt
SET default_sla_policy_id = 'SLA_' || bt.case_type || '_48H'
WHERE bt.case_type LIKE 'LNM_%_V2' AND bt.case_type <> 'LOAN_FORMATION_V2';

-- +goose Down

DELETE FROM business_sla_task_policies WHERE sla_policy_id IN ('SLA_LNM_DEBT_CHANGE_V2', 'SLA_LNM_RATE_CHANGE_V2', 'SLA_LNM_RESTRUCTURE_V2', 'SLA_LNM_WAIVER_V2', 'SLA_LNM_WRITEOFF_V2', 'SLA_LNM_RECOVERY_V2', 'SLA_LNM_FUND_CHECK_V2', 'SLA_LNM_REVENUE_ALLOCATION_V2', 'SLA_LNM_VFU_FEE_ALLOCATION_V2', 'SLA_LNM_OFF_BALANCE_EXPORT_V2');
DELETE FROM business_sla_policies WHERE case_type IN ('LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2', 'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2', 'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2');
DELETE FROM business_process_roles WHERE case_type IN ('LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2', 'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2', 'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2');
DELETE FROM workflow_assignment_rules WHERE case_type IN ('LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2', 'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2', 'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2');
DELETE FROM business_operation_types WHERE case_type IN ('LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2', 'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2', 'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2');
