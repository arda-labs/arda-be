-- +goose Up

-- P1b v2 / Wave 3: split the disbursement flow into the EPAS two-flow
-- register/complete pair. The single-phase LNM_DISBURSEMENT_V2 operation is
-- retired (row kept for FK history; GetCaseType only accepts ACTIVE, so no
-- new cases can start it). Seed shape follows 20260907100100.

UPDATE business_operation_types
SET status = 'RETIRED', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_DISBURSEMENT_V2';

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('LNM_DISB_REGISTER_V2', 'CREDIT', 'Đăng ký giải ngân (v2)', 'lnm-disbursement-register-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_DISB_COMPLETE_V2', 'CREDIT', 'Hoàn tất giải ngân (v2)', 'lnm-disbursement-complete-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

-- Assignment rules: maker opens, checker approves with separation of duties
-- (fallback LNM_POGD). LNM_MAKER / LNM_CHECKER already exist in
-- workflow_role_catalog — do not re-insert.
INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_LNM_DISB_REGISTER_MAKER',   'LNM_DISB_REGISTER_V2', 'maker_input',    'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_LNM_DISB_REGISTER_CHECKER', 'LNM_DISB_REGISTER_V2', 'checker_review', 'LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_DISB_COMPLETE_MAKER',   'LNM_DISB_COMPLETE_V2', 'maker_input',    'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_LNM_DISB_COMPLETE_CHECKER', 'LNM_DISB_COMPLETE_V2', 'checker_review', 'LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_LNM_DISB_REGISTER_MAKER',   'LNM_DISB_REGISTER_V2', 'maker_input',    'Nhập hồ sơ giải ngân',       'LNM_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_DISB_REGISTER_CHECKER', 'LNM_DISB_REGISTER_V2', 'checker_review', 'Phê duyệt giải ngân',        'LNM_CHECKER','OPEN,APPROVE',     'ACTIVE'),
    ('BPR_LNM_DISB_COMPLETE_MAKER',   'LNM_DISB_COMPLETE_V2', 'maker_input',    'Nhập hồ sơ hoàn tất giải ngân','LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_DISB_COMPLETE_CHECKER', 'LNM_DISB_COMPLETE_V2', 'checker_review', 'Phê duyệt hoàn tất giải ngân', 'LNM_CHECKER','OPEN,APPROVE',    'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- SLA: one 48h policy per flow + task rows, matching the disbursement family.
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_LNM_DISB_REGISTER_V2_48H', 'SLA_LNM_DISB_REGISTER_V2_48H', 'Đăng ký giải ngân 48h', 'LNM_DISB_REGISTER_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_DISB_COMPLETE_V2_48H', 'SLA_LNM_DISB_COMPLETE_V2_48H', 'Hoàn tất giải ngân 48h', 'LNM_DISB_COMPLETE_V2', 48, 8, 'LNM_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_LNM_DISB_REGISTER_MAKER',   'SLA_LNM_DISB_REGISTER_V2_48H', 'maker_input',    'Nhập hồ sơ giải ngân',        4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_LNM_DISB_REGISTER_CHECKER', 'SLA_LNM_DISB_REGISTER_V2_48H', 'checker_review', 'Phê duyệt giải ngân',         8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_LNM_DISB_COMPLETE_MAKER',   'SLA_LNM_DISB_COMPLETE_V2_48H', 'maker_input',    'Nhập hồ sơ hoàn tất giải ngân', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_LNM_DISB_COMPLETE_CHECKER', 'SLA_LNM_DISB_COMPLETE_V2_48H', 'checker_review', 'Phê duyệt hoàn tất giải ngân',  8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
