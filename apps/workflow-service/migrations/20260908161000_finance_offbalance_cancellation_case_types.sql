-- +goose Up

-- Arda iteration 10: off-balance memo posting (nhập xuất ngoại bảng) and
-- transaction cancellation (hủy giao dịch). Roles reuse the FIN catalog rows
-- seeded by 20260908140000 (FIN_MAKER / FIN_CHECKER / FIN_POGD); only the
-- two new case types need assignment / business-role / SLA rows.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('FIN_OFF_BALANCE_V2', 'FINANCE', 'Ngoại bảng (v2)',    'fin-off-balance-v2', 1, TRUE, 'FIN_MAKER', 'FIN_CHECKER', 'finance-service', 'ACTIVE'),
    ('FIN_TXN_CANCEL_V2',  'FINANCE', 'Hủy giao dịch (v2)', 'fin-txn-cancel-v2',  1, TRUE, 'FIN_MAKER', 'FIN_CHECKER', 'finance-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

-- Assignment rules: maker opens, checker approves with separation of duties
-- (fallback FIN_POGD) — same shape as the iteration 9 manual posting flows.
INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_FIN_OFF_BALANCE_MAKER',   'FIN_OFF_BALANCE_V2', 'maker_input',    'FIN_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_FIN_OFF_BALANCE_CHECKER', 'FIN_OFF_BALANCE_V2', 'checker_review', 'FIN_CHECKER','CANDIDATE_POOL', TRUE,  'FIN_POGD', 20, 'ACTIVE'),
    ('ASSIGN_FIN_TXN_CANCEL_MAKER',    'FIN_TXN_CANCEL_V2',  'maker_input',    'FIN_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_FIN_TXN_CANCEL_CHECKER',  'FIN_TXN_CANCEL_V2',  'checker_review', 'FIN_CHECKER','CANDIDATE_POOL', TRUE,  'FIN_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_FIN_OFF_BALANCE_MAKER',   'FIN_OFF_BALANCE_V2', 'maker_input',    'Nhập xuất ngoại bảng',  'FIN_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_FIN_OFF_BALANCE_CHECKER', 'FIN_OFF_BALANCE_V2', 'checker_review', 'Phê duyệt ngoại bảng',  'FIN_CHECKER','OPEN,APPROVE',     'ACTIVE'),
    ('BPR_FIN_TXN_CANCEL_MAKER',    'FIN_TXN_CANCEL_V2',  'maker_input',    'Nhập yêu cầu hủy',      'FIN_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_FIN_TXN_CANCEL_CHECKER',  'FIN_TXN_CANCEL_V2',  'checker_review', 'Phê duyệt hủy giao dịch','FIN_CHECKER','OPEN,APPROVE',    'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- SLA: one 48h policy per flow + task rows, matching the manual posting family.
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_FIN_OFF_BALANCE_V2_48H', 'SLA_FIN_OFF_BALANCE_V2_48H', 'Ngoại bảng 48h',    'FIN_OFF_BALANCE_V2', 48, 8, 'FIN_POGD', 'ACTIVE'),
    ('SLA_FIN_TXN_CANCEL_V2_48H',  'SLA_FIN_TXN_CANCEL_V2_48H',  'Hủy giao dịch 48h', 'FIN_TXN_CANCEL_V2',  48, 8, 'FIN_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_FIN_OFF_BALANCE_MAKER',   'SLA_FIN_OFF_BALANCE_V2_48H', 'maker_input',    'Nhập xuất ngoại bảng',  4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_FIN_OFF_BALANCE_CHECKER', 'SLA_FIN_OFF_BALANCE_V2_48H', 'checker_review', 'Phê duyệt ngoại bảng',  8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'FIN_POGD', 20, 'ACTIVE'),
    ('SLA_FIN_TXN_CANCEL_MAKER',    'SLA_FIN_TXN_CANCEL_V2_48H',  'maker_input',    'Nhập yêu cầu hủy',      4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_FIN_TXN_CANCEL_CHECKER',  'SLA_FIN_TXN_CANCEL_V2_48H',  'checker_review', 'Phê duyệt hủy giao dịch', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'FIN_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
