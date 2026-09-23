-- +goose Up

-- Interbank BORROWING case type (step 6b-2). It reuses the placement BPMN
-- shape: maker input -> validate -> checker review -> execute. Only the object
-- differs (an ibm_borrows row rather than an ibm_deposits one), so
-- business_operation_types points at the existing ibm-place-v1 process instead
-- of deploying a near-identical BPMN.
--
-- Roles IBM_MAKER / IBM_CHECKER already exist in workflow_role_catalog (seeded
-- with the placement case types), so they are not re-seeded here.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('IBM_BORROW_V1', 'DEPOSIT', 'Đề nghị vay vốn TCTD khác (v1)', 'ibm-place-v1', 1, TRUE, 'IBM_MAKER', 'IBM_CHECKER', 'deposit-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_IBM_BORROW_MAKER',   'IBM_BORROW_V1', 'UT_MakerInput',    'IBM_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_IBM_BORROW_CHECKER', 'IBM_BORROW_V1', 'UT_CheckerReview', 'IBM_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_IBM_BORROW_MAKER',   'IBM_BORROW_V1', 'UT_MakerInput',    'Lập đề nghị vay TCTD khác',  'IBM_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_IBM_BORROW_CHECKER', 'IBM_BORROW_V1', 'UT_CheckerReview', 'Phê duyệt đề nghị vay TCTD', 'IBM_CHECKER', 'OPEN,APPROVE', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_IBM_BORROW_V1_24H', 'SLA_IBM_BORROW_V1_24H', 'Đề nghị vay TCTD khác 24h', 'IBM_BORROW_V1', 24, 4, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_IBM_BORROW_MAKER',   'SLA_IBM_BORROW_V1_24H', 'UT_MakerInput',    'Lập đề nghị vay TCTD khác',  4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_IBM_BORROW_CHECKER', 'SLA_IBM_BORROW_V1_24H', 'UT_CheckerReview', 'Phê duyệt đề nghị vay TCTD', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM business_sla_task_policies
 WHERE id IN ('SLA_IBM_BORROW_MAKER', 'SLA_IBM_BORROW_CHECKER');
DELETE FROM business_sla_policies WHERE id = 'SLA_IBM_BORROW_V1_24H';
DELETE FROM business_process_roles
 WHERE id IN ('BPR_IBM_BORROW_MAKER', 'BPR_IBM_BORROW_CHECKER');
DELETE FROM workflow_assignment_rules
 WHERE case_type = 'IBM_BORROW_V1';
DELETE FROM business_operation_types WHERE case_type = 'IBM_BORROW_V1';
