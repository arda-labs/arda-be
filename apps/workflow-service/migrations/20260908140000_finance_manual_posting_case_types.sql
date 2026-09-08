-- +goose Up

-- Arda iteration 9: FAC-native manual posting flows (bút toán lẻ / bút toán
-- kép). The FE submits accountant-picked lines through finance-service; the
-- case rides the finance two-phase posting lifecycle (Reserve → Validate →
-- Post / Release). Seed shape follows 20260908120000 (disbursement pair).

-- Role catalog rows must exist first: workflow_assignment_rules.role_code
-- carries an FK to workflow_role_catalog (same pattern as the deposit seed).
INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('FIN_MAKER',  'Finance maker - lập bút toán',        'MAKER',  'FIN', 'ACTIVE'),
    ('FIN_CHECKER','Finance checker - phê duyệt bút toán', 'CHECKER','FIN', 'ACTIVE'),
    ('FIN_POGD',   'Finance phòng giao dịch - xử lý bút toán', 'CHECKER', 'FIN', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('FIN_SINGLE_ENTRY_V2', 'FINANCE', 'Bút toán lẻ (v2)', 'fin-single-entry-v2', 1, TRUE, 'FIN_MAKER', 'FIN_CHECKER', 'finance-service', 'ACTIVE'),
    ('FIN_DOUBLE_ENTRY_V2', 'FINANCE', 'Bút toán kép (v2)', 'fin-double-entry-v2', 1, TRUE, 'FIN_MAKER', 'FIN_CHECKER', 'finance-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

-- Assignment rules: maker opens, checker approves with separation of duties
-- (fallback FIN_POGD).
INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_FIN_SINGLE_ENTRY_MAKER',   'FIN_SINGLE_ENTRY_V2', 'maker_input',    'FIN_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_FIN_SINGLE_ENTRY_CHECKER', 'FIN_SINGLE_ENTRY_V2', 'checker_review', 'FIN_CHECKER','CANDIDATE_POOL', TRUE,  'FIN_POGD', 20, 'ACTIVE'),
    ('ASSIGN_FIN_DOUBLE_ENTRY_MAKER',   'FIN_DOUBLE_ENTRY_V2', 'maker_input',    'FIN_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_FIN_DOUBLE_ENTRY_CHECKER', 'FIN_DOUBLE_ENTRY_V2', 'checker_review', 'FIN_CHECKER','CANDIDATE_POOL', TRUE,  'FIN_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_FIN_SINGLE_ENTRY_MAKER',   'FIN_SINGLE_ENTRY_V2', 'maker_input',    'Nhập bút toán lẻ',      'FIN_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_FIN_SINGLE_ENTRY_CHECKER', 'FIN_SINGLE_ENTRY_V2', 'checker_review', 'Phê duyệt bút toán lẻ', 'FIN_CHECKER','OPEN,APPROVE',     'ACTIVE'),
    ('BPR_FIN_DOUBLE_ENTRY_MAKER',   'FIN_DOUBLE_ENTRY_V2', 'maker_input',    'Nhập bút toán kép',     'FIN_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_FIN_DOUBLE_ENTRY_CHECKER', 'FIN_DOUBLE_ENTRY_V2', 'checker_review', 'Phê duyệt bút toán kép', 'FIN_CHECKER','OPEN,APPROVE',    'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- SLA: one 48h policy per flow + task rows, matching the disbursement family.
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_FIN_SINGLE_ENTRY_V2_48H', 'SLA_FIN_SINGLE_ENTRY_V2_48H', 'Bút toán lẻ 48h', 'FIN_SINGLE_ENTRY_V2', 48, 8, 'FIN_POGD', 'ACTIVE'),
    ('SLA_FIN_DOUBLE_ENTRY_V2_48H', 'SLA_FIN_DOUBLE_ENTRY_V2_48H', 'Bút toán kép 48h', 'FIN_DOUBLE_ENTRY_V2', 48, 8, 'FIN_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_FIN_SINGLE_ENTRY_MAKER',   'SLA_FIN_SINGLE_ENTRY_V2_48H', 'maker_input',    'Nhập bút toán lẻ',      4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_FIN_SINGLE_ENTRY_CHECKER', 'SLA_FIN_SINGLE_ENTRY_V2_48H', 'checker_review', 'Phê duyệt bút toán lẻ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'FIN_POGD', 20, 'ACTIVE'),
    ('SLA_FIN_DOUBLE_ENTRY_MAKER',   'SLA_FIN_DOUBLE_ENTRY_V2_48H', 'maker_input',    'Nhập bút toán kép',     4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_FIN_DOUBLE_ENTRY_CHECKER', 'SLA_FIN_DOUBLE_ENTRY_V2_48H', 'checker_review', 'Phê duyệt bút toán kép', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'FIN_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
