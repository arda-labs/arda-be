-- +goose Up

-- CFM (vốn nội bộ) case types: formation / amendment / movement. Maker submits
-- the staged object in capital-service; checker APPROVE applies it (movement
-- posting rides provisional CFC_* rule cards).

INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('CFC_MAKER',   'Capital maker - lập giao dịch vốn',  'MAKER',   'CFC', 'ACTIVE'),
    ('CFC_CHECKER', 'Capital checker - duyệt giao dịch vốn', 'CHECKER', 'CFC', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('CFC_CONTRACT_V1',  'CAPITAL', 'Hình thành hợp đồng vốn (v1)',   'cfc-contract-v1',  1, TRUE, 'CFC_MAKER', 'CFC_CHECKER', 'capital-service', 'ACTIVE'),
    ('CFC_AMENDMENT_V1', 'CAPITAL', 'Điều chỉnh hợp đồng vốn (v1)',   'cfc-amendment-v1', 1, TRUE, 'CFC_MAKER', 'CFC_CHECKER', 'capital-service', 'ACTIVE'),
    ('CFC_MOVEMENT_V1',  'CAPITAL', 'Giao dịch vốn (v1)',             'cfc-movement-v1',  1, TRUE, 'CFC_MAKER', 'CFC_CHECKER', 'capital-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_CFC_CONTRACT_MAKER',   'CFC_CONTRACT_V1',  'UT_MakerInput',    'CFC_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_CFC_CONTRACT_CHECKER', 'CFC_CONTRACT_V1',  'UT_CheckerReview', 'CFC_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_CFC_AMENDMENT_MAKER',  'CFC_AMENDMENT_V1', 'UT_MakerInput',    'CFC_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_CFC_AMENDMENT_CHECK',  'CFC_AMENDMENT_V1', 'UT_CheckerReview', 'CFC_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE'),
    ('ASSIGN_CFC_MOVEMENT_MAKER',   'CFC_MOVEMENT_V1',  'UT_MakerInput',    'CFC_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_CFC_MOVEMENT_CHECKER', 'CFC_MOVEMENT_V1',  'UT_CheckerReview', 'CFC_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_CFC_CONTRACT_MAKER',   'CFC_CONTRACT_V1',  'UT_MakerInput',    'Nhập hợp đồng vốn',      'CFC_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_CFC_CONTRACT_CHECKER', 'CFC_CONTRACT_V1',  'UT_CheckerReview', 'Phê duyệt hợp đồng vốn', 'CFC_CHECKER', 'OPEN,APPROVE',     'ACTIVE'),
    ('BPR_CFC_AMENDMENT_MAKER',  'CFC_AMENDMENT_V1', 'UT_MakerInput',    'Nhập điều chỉnh hợp đồng vốn', 'CFC_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_CFC_AMENDMENT_CHECK',  'CFC_AMENDMENT_V1', 'UT_CheckerReview', 'Phê duyệt điều chỉnh hợp đồng vốn', 'CFC_CHECKER', 'OPEN,APPROVE', 'ACTIVE'),
    ('BPR_CFC_MOVEMENT_MAKER',   'CFC_MOVEMENT_V1',  'UT_MakerInput',    'Lập giao dịch vốn',      'CFC_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_CFC_MOVEMENT_CHECKER', 'CFC_MOVEMENT_V1',  'UT_CheckerReview', 'Phê duyệt giao dịch vốn', 'CFC_CHECKER', 'OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_CFC_CONTRACT_V1_24H',  'SLA_CFC_CONTRACT_V1_24H',  'Hợp đồng vốn 24h',   'CFC_CONTRACT_V1',  24, 4, '', 'ACTIVE'),
    ('SLA_CFC_AMENDMENT_V1_24H', 'SLA_CFC_AMENDMENT_V1_24H', 'Điều chỉnh vốn 24h', 'CFC_AMENDMENT_V1', 24, 4, '', 'ACTIVE'),
    ('SLA_CFC_MOVEMENT_V1_24H',  'SLA_CFC_MOVEMENT_V1_24H',  'Giao dịch vốn 24h',  'CFC_MOVEMENT_V1',  24, 4, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_CFC_CONTRACT_MAKER',   'SLA_CFC_CONTRACT_V1_24H',  'UT_MakerInput',    'Nhập hợp đồng vốn',      4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_CFC_CONTRACT_CHECKER', 'SLA_CFC_CONTRACT_V1_24H',  'UT_CheckerReview', 'Phê duyệt hợp đồng vốn', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_CFC_AMEND_MAKER',      'SLA_CFC_AMENDMENT_V1_24H', 'UT_MakerInput',    'Nhập điều chỉnh hợp đồng vốn',   4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_CFC_AMEND_CHECKER',    'SLA_CFC_AMENDMENT_V1_24H', 'UT_CheckerReview', 'Phê duyệt điều chỉnh hợp đồng vốn', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE'),
    ('SLA_CFC_MOVE_MAKER',       'SLA_CFC_MOVEMENT_V1_24H',  'UT_MakerInput',    'Lập giao dịch vốn',      4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_CFC_MOVE_CHECKER',     'SLA_CFC_MOVEMENT_V1_24H',  'UT_CheckerReview', 'Phê duyệt giao dịch vốn', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down

DELETE FROM business_sla_task_policies WHERE id LIKE 'SLA_CFC_%';
DELETE FROM business_sla_policies WHERE id LIKE 'SLA_CFC_%';
DELETE FROM business_process_roles WHERE id LIKE 'BPR_CFC_%';
DELETE FROM workflow_assignment_rules WHERE id LIKE 'ASSIGN_CFC_%';
DELETE FROM business_operation_types WHERE case_type IN ('CFC_CONTRACT_V1', 'CFC_AMENDMENT_V1', 'CFC_MOVEMENT_V1');
DELETE FROM workflow_role_catalog WHERE role_code IN ('CFC_MAKER', 'CFC_CHECKER');
