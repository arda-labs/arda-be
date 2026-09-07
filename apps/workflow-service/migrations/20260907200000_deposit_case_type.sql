-- +goose Up

-- P2.8a: DPM_SETTLE_V2 case-type + assignment rules + SLA.

-- Role catalog rows must exist first: workflow_assignment_rules.role_code
-- carries an FK to workflow_role_catalog (same pattern as the loan seeds).
INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('DPM_MAKER',  'Deposit maker - lập phiếu tất toán',   'MAKER',  'DPM', 'ACTIVE'),
    ('DPM_CHECKER','Deposit checker - phê duyệt tất toán', 'CHECKER','DPM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('DPM_SETTLE_V2', 'DEPOSIT', 'Tất toán sổ tiết kiệm (v2)', 'dpm-settle-v2', 1, TRUE, 'DPM_MAKER', 'DPM_CHECKER', 'deposit-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_DPM_SETTLE_MAKER',  'DPM_SETTLE_V2', 'maker_input',   'DPM_MAKER',  'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_DPM_SETTLE_CHECKER','DPM_SETTLE_V2', 'checker_review','DPM_CHECKER','CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_DPM_SETTLE_V2_48H', 'SLA_DPM_SETTLE_V2_48H', 'Tất toán 48h', 'DPM_SETTLE_V2', 48, 8, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_TASK_DPM_SETTLE_INPUT',   'SLA_DPM_SETTLE_V2_48H', 'maker_input',    'Nhập hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_TASK_DPM_SETTLE_APPROVE', 'SLA_DPM_SETTLE_V2_48H', 'checker_review', 'Phê duyệt', 16, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
SELECT 1;
