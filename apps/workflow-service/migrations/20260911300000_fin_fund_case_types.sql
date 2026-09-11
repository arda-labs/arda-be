-- +goose Up

-- Quỹ (fund) case types: trích lập (FIN_FUND_APPROP_V2) + sử dụng
-- (FIN_FUND_USE_V2). Roles reuse the FIN_MAKER / FIN_CHECKER rows the finance
-- manual-posting/closing flows seeded; task rows key on the BPMN element ids.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('FIN_FUND_APPROP_V2', 'FINANCE', 'Trích lập quỹ (v2)', 'fin-fund-appropriation-v2', 1, TRUE, 'FIN_MAKER', 'FIN_CHECKER', 'finance-service', 'ACTIVE'),
    ('FIN_FUND_USE_V2',    'FINANCE', 'Sử dụng quỹ (v2)',   'fin-fund-utilization-v2',   1, TRUE, 'FIN_MAKER', 'FIN_CHECKER', 'finance-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_FIN_FUND_APPROP_MAKER',   'FIN_FUND_APPROP_V2', 'UT_MakerInput',    'FIN_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_FIN_FUND_APPROP_CHECKER', 'FIN_FUND_APPROP_V2', 'UT_CheckerReview', 'FIN_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_FIN_FUND_USE_MAKER',      'FIN_FUND_USE_V2',    'UT_MakerInput',    'FIN_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_FIN_FUND_USE_CHECKER',    'FIN_FUND_USE_V2',    'UT_CheckerReview', 'FIN_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_FIN_FUND_APPROP_MAKER',   'FIN_FUND_APPROP_V2', 'UT_MakerInput',    'Trích lập quỹ',   'FIN_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_FIN_FUND_APPROP_CHECKER', 'FIN_FUND_APPROP_V2', 'UT_CheckerReview', 'Phê duyệt trích lập quỹ', 'FIN_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_FIN_FUND_USE_MAKER',      'FIN_FUND_USE_V2',    'UT_MakerInput',    'Sử dụng quỹ',     'FIN_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_FIN_FUND_USE_CHECKER',    'FIN_FUND_USE_V2',    'UT_CheckerReview', 'Phê duyệt sử dụng quỹ',   'FIN_CHECKER', 'OPEN,APPROVE', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_FIN_FUND_APPROP_48H', 'SLA_FIN_FUND_APPROP_48H', 'Trích lập quỹ 48h', 'FIN_FUND_APPROP_V2', 48, 8, '', 'ACTIVE'),
    ('SLA_FIN_FUND_USE_48H',    'SLA_FIN_FUND_USE_48H',    'Sử dụng quỹ 48h',   'FIN_FUND_USE_V2',    48, 8, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_FIN_FUND_APPROP_MAKER',   'SLA_FIN_FUND_APPROP_48H', 'UT_MakerInput',    'Trích lập quỹ', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_FIN_FUND_APPROP_CHECKER', 'SLA_FIN_FUND_APPROP_48H', 'UT_CheckerReview', 'Phê duyệt trích lập quỹ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_FIN_FUND_USE_MAKER',      'SLA_FIN_FUND_USE_48H',    'UT_MakerInput',    'Sử dụng quỹ',   4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_FIN_FUND_USE_CHECKER',    'SLA_FIN_FUND_USE_48H',    'UT_CheckerReview', 'Phê duyệt sử dụng quỹ',   8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
