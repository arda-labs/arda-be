-- +goose Up

-- Arda iteration 11: closing (kết chuyển thu chi, FAC.203.01). Roles reuse
-- the FIN catalog rows seeded by 20260908140000 (FIN_MAKER / FIN_CHECKER /
-- FIN_POGD); only the new case type needs assignment / business-role / SLA
-- rows — mirror of 20260908161000_finance_offbalance_cancellation_case_types.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('FIN_CLOSING_V2', 'FINANCE', 'Kết chuyển thu chi (v2)', 'fin-closing-v2', 1, TRUE, 'FIN_MAKER', 'FIN_CHECKER', 'finance-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

-- Assignment rules: maker opens, checker approves with separation of duties
-- (fallback FIN_POGD) — same shape as the manual posting family.
INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_FIN_CLOSING_MAKER',   'FIN_CLOSING_V2', 'maker_input',    'FIN_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_FIN_CLOSING_CHECKER', 'FIN_CLOSING_V2', 'checker_review', 'FIN_CHECKER','CANDIDATE_POOL', TRUE,  'FIN_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_FIN_CLOSING_MAKER',   'FIN_CLOSING_V2', 'maker_input',    'Nhập kết chuyển thu chi', 'FIN_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_FIN_CLOSING_CHECKER', 'FIN_CLOSING_V2', 'checker_review', 'Phê duyệt kết chuyển',    'FIN_CHECKER','OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- SLA: one 48h policy per flow + task rows, matching the manual posting family.
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_FIN_CLOSING_V2_48H', 'SLA_FIN_CLOSING_V2_48H', 'Kết chuyển thu chi 48h', 'FIN_CLOSING_V2', 48, 8, 'FIN_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_FIN_CLOSING_MAKER',   'SLA_FIN_CLOSING_V2_48H', 'maker_input',    'Nhập kết chuyển thu chi', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_FIN_CLOSING_CHECKER', 'SLA_FIN_CLOSING_V2_48H', 'checker_review', 'Phê duyệt kết chuyển',    8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'FIN_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
