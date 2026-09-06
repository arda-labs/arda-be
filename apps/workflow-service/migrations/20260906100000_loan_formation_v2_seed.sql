-- +goose Up

-- LOAN_FORMATION_V2: reference case-type proving multi-level approval on the
-- Arda platform, derived from EPAS LNM.201.01 (Hình thành khoản vay). The loan
-- domain service itself is P1 scope; this seed only registers flow + roles.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('LOAN_FORMATION_V2', 'CREDIT', 'Hình thành khoản vay đa cấp (mẫu LNM.201.01)',
     'lnm-loan-formation-v2', 1, TRUE, 'LNM_MAKER', 'LNM_POGD', 'loan-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id,
    bpmn_version = EXCLUDED.bpmn_version,
    operation_name = EXCLUDED.operation_name,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('LNM_MAKER', 'Credit maker - nhập hồ sơ khoản vay', 'MAKER', 'LNM', 'ACTIVE'),
    ('LNM_TWTD', 'Tái thẩm định - Trung ương', 'REVIEWER', 'LNM', 'ACTIVE'),
    ('LNM_POGD', 'Phó giám đốc - xem xét khoản vay', 'CHECKER', 'LNM', 'ACTIVE'),
    ('LNM_GIDO', 'Giám đốc - phê duyệt khoản vay', 'CHECKER', 'LNM', 'ACTIVE'),
    ('LNM_HODO', 'Hội đồng tín dụng - phê duyệt hạn mức lớn', 'BOARD', 'LNM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

-- Assignment rules per step: pool mode for review steps, separation of duties
-- for every approval tier (the maker must never review their own file).
INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_LNM_MAKER_INPUT',    'LOAN_FORMATION_V2', 'maker_input',   'LNM_MAKER', 'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_TW_REVALIDATE',  'LOAN_FORMATION_V2', 'tw_revalidate', 'LNM_TWTD',  'CANDIDATE_POOL', TRUE,  '', 10, 'ACTIVE'),
    ('ASSIGN_LNM_PGD_REVIEW',     'LOAN_FORMATION_V2', 'pgd_review',    'LNM_POGD',  'CANDIDATE_POOL', TRUE,  'LNM_TWTD',  10, 'ACTIVE'),
    ('ASSIGN_LNM_GD_REVIEW',      'LOAN_FORMATION_V2', 'gd_review',     'LNM_GIDO',  'DIRECT',         TRUE,  'LNM_POGD',  10, 'ACTIVE'),
    ('ASSIGN_LNM_BOARD_REVIEW',   'LOAN_FORMATION_V2', 'board_review',  'LNM_HODO',  'CANDIDATE_POOL', TRUE,  'LNM_GIDO',  10, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_LNM_MAKER_INPUT',  'LOAN_FORMATION_V2', 'maker_input',   'Nhập hồ sơ khoản vay', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_TW_REVALIDATE','LOAN_FORMATION_V2', 'tw_revalidate', 'Tái thẩm định Trung ương', 'LNM_TWTD', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_PGD_REVIEW',   'LOAN_FORMATION_V2', 'pgd_review',    'Xem xét cấp Phó giám đốc', 'LNM_POGD', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_GD_REVIEW',    'LOAN_FORMATION_V2', 'gd_review',     'Phê duyệt cấp Giám đốc', 'LNM_GIDO', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_LNM_BOARD_REVIEW', 'LOAN_FORMATION_V2', 'board_review',  'Phê duyệt Hội đồng tín dụng', 'LNM_HODO', 'OPEN,APPROVE', 'ACTIVE')
ON CONFLICT (case_type, step_code, iam_role) DO NOTHING;

-- SLA: mirrors EPAS COM_CFG_SLA_PROCESS semantics on the Arda-side policies.
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_LOAN_FORMATION_V2_72H', 'SLA_LOAN_FORMATION_V2_72H', 'Hình thành khoản vay đa cấp 72h', 'LOAN_FORMATION_V2', 72, 12, 'LNM_GIDO', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    case_type = EXCLUDED.case_type,
    due_in_hours = EXCLUDED.due_in_hours,
    warning_in_hours = EXCLUDED.warning_in_hours,
    escalation_role = EXCLUDED.escalation_role,
    status = EXCLUDED.status,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_TASK_LNM_INPUT',        'SLA_LOAN_FORMATION_V2_72H', 'maker_input',   'Nhập hồ sơ khoản vay', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 10, 'ACTIVE'),
    ('SLA_TASK_LNM_TW_REVALIDATE','SLA_LOAN_FORMATION_V2_72H', 'tw_revalidate', 'Tái thẩm định Trung ương', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_GIDO', 20, 'ACTIVE'),
    ('SLA_TASK_LNM_PGD_REVIEW',   'SLA_LOAN_FORMATION_V2_72H', 'pgd_review',    'Xem xét cấp Phó giám đốc', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_GIDO', 30, 'ACTIVE'),
    ('SLA_TASK_LNM_GD_REVIEW',    'SLA_LOAN_FORMATION_V2_72H', 'gd_review',     'Phê duyệt cấp Giám đốc', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_HODO', 40, 'ACTIVE'),
    ('SLA_TASK_LNM_BOARD_REVIEW', 'SLA_LOAN_FORMATION_V2_72H', 'board_review',  'Phê duyệt Hội đồng tín dụng', 24, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_HODO', 50, 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET
    sla_policy_id = EXCLUDED.sla_policy_id,
    step_code = EXCLUDED.step_code,
    task_name = EXCLUDED.task_name,
    duration_value = EXCLUDED.duration_value,
    duration_unit = EXCLUDED.duration_unit,
    warning_mode = EXCLUDED.warning_mode,
    warning_value = EXCLUDED.warning_value,
    warning_unit = EXCLUDED.warning_unit,
    escalation_role = EXCLUDED.escalation_role,
    sort_order = EXCLUDED.sort_order,
    status = EXCLUDED.status,
    updated_at = CURRENT_TIMESTAMP;

UPDATE business_operation_types
SET default_sla_policy_id = 'SLA_LOAN_FORMATION_V2_72H',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2';

-- +goose Down

UPDATE business_operation_types
SET default_sla_policy_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2';

DELETE FROM business_sla_task_policies WHERE id IN (
    'SLA_TASK_LNM_INPUT', 'SLA_TASK_LNM_TW_REVALIDATE', 'SLA_TASK_LNM_PGD_REVIEW',
    'SLA_TASK_LNM_GD_REVIEW', 'SLA_TASK_LNM_BOARD_REVIEW'
);
DELETE FROM business_sla_policies WHERE id = 'SLA_LOAN_FORMATION_V2_72H';
DELETE FROM business_process_roles WHERE case_type = 'LOAN_FORMATION_V2';
DELETE FROM workflow_assignment_rules WHERE case_type = 'LOAN_FORMATION_V2';
DELETE FROM workflow_role_catalog WHERE business_subsystem = 'LNM' AND role_code IN (
    'LNM_MAKER', 'LNM_TWTD', 'LNM_POGD', 'LNM_GIDO', 'LNM_HODO'
);
DELETE FROM business_operation_types WHERE case_type = 'LOAN_FORMATION_V2';
