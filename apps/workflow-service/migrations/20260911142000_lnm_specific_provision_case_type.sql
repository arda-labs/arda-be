-- +goose Up

-- LNM.306 specific provision case type. Roles reuse the LNM catalog rows
-- seeded with loan formation (LNM_MAKER / LNM_CHECKER); task rows key on the
-- BPMN element ids (UT_*).

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('LNM_SPECIFIC_PROVISION_V1', 'LOAN', 'Trích lập dự phòng cụ thể (v1)', 'lnm-specific-provision-v1', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_LNM_SPECIFIC_PROVISION_MAKER',   'LNM_SPECIFIC_PROVISION_V1', 'UT_MakerInput',    'LNM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_SPECIFIC_PROVISION_CHECKER', 'LNM_SPECIFIC_PROVISION_V1', 'UT_CheckerReview', 'LNM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_LNM_SPECIFIC_PROVISION_MAKER',   'LNM_SPECIFIC_PROVISION_V1', 'UT_MakerInput',    'Trích lập dự phòng cụ thể',    'LNM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_SPECIFIC_PROVISION_CHECKER', 'LNM_SPECIFIC_PROVISION_V1', 'UT_CheckerReview', 'Phê duyệt dự phòng cụ thể',    'LNM_CHECKER', 'OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_LNM_SPECIFIC_PROVISION_48H', 'SLA_LNM_SPECIFIC_PROVISION_48H', 'Trích lập dự phòng cụ thể 48h', 'LNM_SPECIFIC_PROVISION_V1', 48, 8, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_LNM_SPECIFIC_PROVISION_MAKER',   'SLA_LNM_SPECIFIC_PROVISION_48H', 'UT_MakerInput',    'Trích lập dự phòng cụ thể', 4,  'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_LNM_SPECIFIC_PROVISION_CHECKER', 'SLA_LNM_SPECIFIC_PROVISION_48H', 'UT_CheckerReview', 'Phê duyệt dự phòng cụ thể', 8,  'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
