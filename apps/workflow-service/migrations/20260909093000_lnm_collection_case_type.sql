-- +goose Up

-- Iteration 11 wave 2: register the collection case type (LNM.301.02) in the
-- business_operation_types allowlist — the flow was submitting cases since
-- P1b.4 but never had an operation row (the loan formation seed at
-- 20260906100000 covers LOAN_FORMATION_V2 only). Seed shape follows
-- 20260907100100_disbursement_case_type; LNM_MAKER / LNM_CHECKER already
-- exist in workflow_role_catalog — do not re-insert.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('LNM_COLLECTION_V2', 'CREDIT', 'Thu nợ (v2)', 'lnm-collection-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_LNM_COLLECTION_MAKER',   'LNM_COLLECTION_V2', 'maker_input',    'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_LNM_COLLECTION_CHECKER', 'LNM_COLLECTION_V2', 'checker_review', 'LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_LNM_COLLECTION_MAKER',   'LNM_COLLECTION_V2', 'maker_input',    'Nhập hồ sơ thu nợ',  'LNM_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_COLLECTION_CHECKER', 'LNM_COLLECTION_V2', 'checker_review', 'Phê duyệt thu nợ',   'LNM_CHECKER','OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- SLA: 48h policy + task rows, matching the disbursement family.
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_LNM_COLLECTION_V2_48H', 'SLA_LNM_COLLECTION_V2_48H', 'Thu nợ 48h', 'LNM_COLLECTION_V2', 48, 8, 'LNM_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_LNM_COLLECTION_MAKER',   'SLA_LNM_COLLECTION_V2_48H', 'maker_input',    'Nhập hồ sơ thu nợ', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_LNM_COLLECTION_CHECKER', 'SLA_LNM_COLLECTION_V2_48H', 'checker_review', 'Phê duyệt thu nợ',  8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
