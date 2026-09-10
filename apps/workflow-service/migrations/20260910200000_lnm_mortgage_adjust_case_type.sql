-- +goose Up

-- LNM.308-ish mortgage adjust (11th adjustment kind): the kind/table/FE
-- screen existed but the case type + BPMN were never seeded, so submitting a
-- mortgage-adjust case failed with an unknown case type. Element ids align
-- with the native userTask runtime (UT_*).

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('LNM_MORTGAGE_ADJUST_V2', 'CREDIT', 'Điều chỉnh TSBĐ (v2)', 'lnm-mortgage-adjust-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_LNM_MORTGAGE_ADJUST_MAKER',   'LNM_MORTGAGE_ADJUST_V2', 'UT_MakerInput',    'LNM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_MORTGAGE_ADJUST_CHECKER', 'LNM_MORTGAGE_ADJUST_V2', 'UT_CheckerReview', 'LNM_CHECKER', 'CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_LNM_MORTGAGE_ADJUST_MAKER',   'LNM_MORTGAGE_ADJUST_V2', 'UT_MakerInput',    'Nhập đề nghị điều chỉnh TSBĐ', 'LNM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_MORTGAGE_ADJUST_CHECKER', 'LNM_MORTGAGE_ADJUST_V2', 'UT_CheckerReview', 'Phê duyệt điều chỉnh TSBĐ',    'LNM_CHECKER', 'OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_LNM_MORTGAGE_ADJUST_48H', 'SLA_LNM_MORTGAGE_ADJUST_48H', 'Điều chỉnh TSBĐ 48h', 'LNM_MORTGAGE_ADJUST_V2', 48, 8, 'LNM_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_LNM_MORTGAGE_ADJUST_MAKER',   'SLA_LNM_MORTGAGE_ADJUST_48H', 'UT_MakerInput',    'Nhập đề nghị điều chỉnh TSBĐ', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_LNM_MORTGAGE_ADJUST_CHECKER', 'SLA_LNM_MORTGAGE_ADJUST_48H', 'UT_CheckerReview', 'Phê duyệt điều chỉnh TSBĐ',    8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
