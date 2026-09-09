-- +goose Up

-- Iteration 13 wave BE: batch (1 hồ sơ — N hợp đồng) case types for the
-- disbursement register/complete + collection flows. Seed shape follows
-- 20260908120000 / 20260909093000; LNM_MAKER / LNM_CHECKER / LNM_POGD already
-- exist in workflow_role_catalog — do not re-insert.

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('LNM_DISB_BATCH_REGISTER_V2', 'CREDIT', 'Đăng ký giải ngân theo hồ sơ (v2)', 'lnm-disb-batch-register-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_DISB_BATCH_COMPLETE_V2', 'CREDIT', 'Hoàn tất giải ngân theo hồ sơ (v2)', 'lnm-disb-batch-complete-v2', 1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE'),
    ('LNM_COLLECTION_BATCH_V2',    'CREDIT', 'Thu nợ theo hồ sơ (v2)',             'lnm-collection-batch-v2',    1, TRUE, 'LNM_MAKER', 'LNM_CHECKER', 'loan-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_LNM_DISB_BATCH_REGISTER_MAKER',   'LNM_DISB_BATCH_REGISTER_V2', 'maker_input',    'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_LNM_DISB_BATCH_REGISTER_CHECKER', 'LNM_DISB_BATCH_REGISTER_V2', 'checker_review', 'LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_DISB_BATCH_COMPLETE_MAKER',   'LNM_DISB_BATCH_COMPLETE_V2', 'maker_input',    'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_LNM_DISB_BATCH_COMPLETE_CHECKER', 'LNM_DISB_BATCH_COMPLETE_V2', 'checker_review', 'LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE'),
    ('ASSIGN_LNM_COLLECTION_BATCH_MAKER',      'LNM_COLLECTION_BATCH_V2',    'maker_input',    'LNM_MAKER',  'CANDIDATE_POOL', FALSE, '',         10, 'ACTIVE'),
    ('ASSIGN_LNM_COLLECTION_BATCH_CHECKER',    'LNM_COLLECTION_BATCH_V2',    'checker_review', 'LNM_CHECKER','CANDIDATE_POOL', TRUE,  'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_LNM_DISB_BATCH_REGISTER_MAKER',   'LNM_DISB_BATCH_REGISTER_V2', 'maker_input',    'Nhập hồ sơ giải ngân theo hợp đồng', 'LNM_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_DISB_BATCH_REGISTER_CHECKER', 'LNM_DISB_BATCH_REGISTER_V2', 'checker_review', 'Phê duyệt giải ngân theo hợp đồng',  'LNM_CHECKER','OPEN,APPROVE',     'ACTIVE'),
    ('BPR_LNM_DISB_BATCH_COMPLETE_MAKER',   'LNM_DISB_BATCH_COMPLETE_V2', 'maker_input',    'Nhập hồ sơ hoàn tất giải ngân theo hợp đồng', 'LNM_MAKER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_DISB_BATCH_COMPLETE_CHECKER', 'LNM_DISB_BATCH_COMPLETE_V2', 'checker_review', 'Phê duyệt hoàn tất giải ngân theo hợp đồng',  'LNM_CHECKER','OPEN,APPROVE',    'ACTIVE'),
    ('BPR_LNM_COLLECTION_BATCH_MAKER',      'LNM_COLLECTION_BATCH_V2',    'maker_input',    'Nhập hồ sơ thu nợ theo hợp đồng',    'LNM_MAKER',  'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_LNM_COLLECTION_BATCH_CHECKER',    'LNM_COLLECTION_BATCH_V2',    'checker_review', 'Phê duyệt thu nợ theo hợp đồng',     'LNM_CHECKER','OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- SLA: one 48h policy per flow + task rows, matching the disbursement family.
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_LNM_DISB_BATCH_REGISTER_V2_48H', 'SLA_LNM_DISB_BATCH_REGISTER_V2_48H', 'Đăng ký giải ngân theo hồ sơ 48h', 'LNM_DISB_BATCH_REGISTER_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_DISB_BATCH_COMPLETE_V2_48H', 'SLA_LNM_DISB_BATCH_COMPLETE_V2_48H', 'Hoàn tất giải ngân theo hồ sơ 48h', 'LNM_DISB_BATCH_COMPLETE_V2', 48, 8, 'LNM_POGD', 'ACTIVE'),
    ('SLA_LNM_COLLECTION_BATCH_V2_48H',    'SLA_LNM_COLLECTION_BATCH_V2_48H',    'Thu nợ theo hồ sơ 48h',             'LNM_COLLECTION_BATCH_V2',    48, 8, 'LNM_POGD', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_LNM_DISB_BATCH_REGISTER_MAKER',   'SLA_LNM_DISB_BATCH_REGISTER_V2_48H', 'maker_input',    'Nhập hồ sơ giải ngân theo hợp đồng',        4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_LNM_DISB_BATCH_REGISTER_CHECKER', 'SLA_LNM_DISB_BATCH_REGISTER_V2_48H', 'checker_review', 'Phê duyệt giải ngân theo hợp đồng',         8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_LNM_DISB_BATCH_COMPLETE_MAKER',   'SLA_LNM_DISB_BATCH_COMPLETE_V2_48H', 'maker_input',    'Nhập hồ sơ hoàn tất giải ngân theo hợp đồng', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_LNM_DISB_BATCH_COMPLETE_CHECKER', 'SLA_LNM_DISB_BATCH_COMPLETE_V2_48H', 'checker_review', 'Phê duyệt hoàn tất giải ngân theo hợp đồng',  8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE'),
    ('SLA_LNM_COLLECTION_BATCH_MAKER',      'SLA_LNM_COLLECTION_BATCH_V2_48H',    'maker_input',    'Nhập hồ sơ thu nợ theo hợp đồng',           4, 'HOUR', 'PERCENT', 75, 'PERCENT', '',         10, 'ACTIVE'),
    ('SLA_LNM_COLLECTION_BATCH_CHECKER',    'SLA_LNM_COLLECTION_BATCH_V2_48H',    'checker_review', 'Phê duyệt thu nợ theo hợp đồng',            8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'LNM_POGD', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
