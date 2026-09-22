-- +goose Up

-- QTDND membership capital movements (CRM_MEMBER_V1): register / additional /
-- withdraw share one case-type. Roles CRM_MAKER / CRM_CHECKER are the maker-
-- checker pair for the member book (distinct from the CRM_AGENT role used by
-- customer registration, so the member book has its own duty owners).
--
-- workflow_assignment_rules.role_code references workflow_role_catalog, so the
-- catalog rows must exist first — the initial version of this migration failed
-- the FK and crash-looped the workflow pod.

INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status)
VALUES
    ('CRM_MAKER',   'CRM member maker - lập yêu cầu vốn góp thành viên', 'MAKER',   'CRM', 'ACTIVE'),
    ('CRM_CHECKER', 'CRM member checker - phê duyệt vốn góp thành viên', 'CHECKER', 'CRM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('CRM_MEMBER_V1', 'CRM', 'Vốn góp thành viên (v1)', 'crm-member-v1', 1, TRUE, 'CRM_MAKER', 'CRM_CHECKER', 'crm-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id,
    operation_name = EXCLUDED.operation_name,
    maker_role = EXCLUDED.maker_role, checker_role = EXCLUDED.checker_role,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_CRM_MEMBER_MAKER',   'CRM_MEMBER_V1', 'UT_MakerInput',    'CRM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_CRM_MEMBER_CHECKER', 'CRM_MEMBER_V1', 'UT_CheckerReview', 'CRM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_CRM_MEMBER_MAKER',   'CRM_MEMBER_V1', 'UT_MakerInput',    'Lập yêu cầu vốn góp thành viên', 'CRM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_CRM_MEMBER_CHECKER', 'CRM_MEMBER_V1', 'UT_CheckerReview', 'Phê duyệt vốn góp thành viên',   'CRM_CHECKER', 'OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_CRM_MEMBER_48H', 'SLA_CRM_MEMBER_48H', 'Vốn góp thành viên 48h', 'CRM_MEMBER_V1', 48, 8, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_CRM_MEMBER_MAKER',   'SLA_CRM_MEMBER_48H', 'UT_MakerInput',    'Lập yêu cầu vốn góp', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_CRM_MEMBER_CHECKER', 'SLA_CRM_MEMBER_48H', 'UT_CheckerReview', 'Phê duyệt vốn góp',   8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
