-- +goose Up

-- v2 step-code alignment + HRM/RPT role seeds.
--
-- The native userTask runtime resolves assignment rules / process roles / SLA
-- task policies by the BPMN element id (UT_MakerInput, UT_CheckerReview, ...)
-- because the projector passes ut.ElementID. Seeds written before the native
-- runtime used the logical task-header stepCode (maker_input, checker_review),
-- so those rows never resolved. CRM already migrated its rows to UT_* in
-- 20260707120000 — align every other v2 case type here.

UPDATE workflow_assignment_rules SET step_code = 'UT_MakerInput', updated_at = CURRENT_TIMESTAMP
WHERE step_code = 'maker_input' AND case_type IN (
    'LOAN_FORMATION_V2',
    'LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2',
    'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2',
    'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2',
    'LNM_DISB_REGISTER_V2', 'LNM_DISB_COMPLETE_V2', 'LNM_DISB_BATCH_REGISTER_V2',
    'LNM_DISB_BATCH_COMPLETE_V2', 'LNM_COLLECTION_V2', 'LNM_COLLECTION_BATCH_V2',
    'FIN_SINGLE_ENTRY_V2', 'FIN_DOUBLE_ENTRY_V2', 'FIN_OFF_BALANCE_V2', 'FIN_TXN_CANCEL_V2',
    'FIN_CLOSING_V2', 'DPM_SETTLE_V2'
);

UPDATE workflow_assignment_rules SET step_code = 'UT_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE step_code = 'checker_review' AND case_type IN (
    'LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2',
    'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2',
    'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2',
    'LNM_DISB_REGISTER_V2', 'LNM_DISB_COMPLETE_V2', 'LNM_DISB_BATCH_REGISTER_V2',
    'LNM_DISB_BATCH_COMPLETE_V2', 'LNM_COLLECTION_V2', 'LNM_COLLECTION_BATCH_V2',
    'FIN_SINGLE_ENTRY_V2', 'FIN_DOUBLE_ENTRY_V2', 'FIN_OFF_BALANCE_V2', 'FIN_TXN_CANCEL_V2',
    'FIN_CLOSING_V2', 'DPM_SETTLE_V2'
);

UPDATE workflow_assignment_rules SET step_code = 'UT_TWRevalidate', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'tw_revalidate';

UPDATE workflow_assignment_rules SET step_code = 'UT_PGDReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'pgd_review';

UPDATE workflow_assignment_rules SET step_code = 'UT_GDReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'gd_review';

UPDATE workflow_assignment_rules SET step_code = 'UT_BoardReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'board_review';

UPDATE business_process_roles SET step_code = 'UT_MakerInput', updated_at = CURRENT_TIMESTAMP
WHERE step_code = 'maker_input' AND case_type IN (
    'LOAN_FORMATION_V2',
    'LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2',
    'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2',
    'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2',
    'LNM_DISB_REGISTER_V2', 'LNM_DISB_COMPLETE_V2', 'LNM_DISB_BATCH_REGISTER_V2',
    'LNM_DISB_BATCH_COMPLETE_V2', 'LNM_COLLECTION_V2', 'LNM_COLLECTION_BATCH_V2',
    'FIN_SINGLE_ENTRY_V2', 'FIN_DOUBLE_ENTRY_V2', 'FIN_OFF_BALANCE_V2', 'FIN_TXN_CANCEL_V2',
    'FIN_CLOSING_V2', 'DPM_SETTLE_V2'
);

UPDATE business_process_roles SET step_code = 'UT_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE step_code = 'checker_review' AND case_type IN (
    'LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2',
    'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2',
    'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2',
    'LNM_DISB_REGISTER_V2', 'LNM_DISB_COMPLETE_V2', 'LNM_DISB_BATCH_REGISTER_V2',
    'LNM_DISB_BATCH_COMPLETE_V2', 'LNM_COLLECTION_V2', 'LNM_COLLECTION_BATCH_V2',
    'FIN_SINGLE_ENTRY_V2', 'FIN_DOUBLE_ENTRY_V2', 'FIN_OFF_BALANCE_V2', 'FIN_TXN_CANCEL_V2',
    'FIN_CLOSING_V2', 'DPM_SETTLE_V2'
);

UPDATE business_process_roles SET step_code = 'UT_TWRevalidate', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'tw_revalidate';

UPDATE business_process_roles SET step_code = 'UT_PGDReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'pgd_review';

UPDATE business_process_roles SET step_code = 'UT_GDReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'gd_review';

UPDATE business_process_roles SET step_code = 'UT_BoardReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LOAN_FORMATION_V2' AND step_code = 'board_review';

UPDATE business_sla_task_policies SET step_code = 'UT_MakerInput', updated_at = CURRENT_TIMESTAMP
WHERE step_code = 'maker_input' AND sla_policy_id IN (
    SELECT id FROM business_sla_policies WHERE case_type IN (
        'LOAN_FORMATION_V2',
        'LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2',
        'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2',
        'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2',
        'LNM_DISB_REGISTER_V2', 'LNM_DISB_COMPLETE_V2', 'LNM_DISB_BATCH_REGISTER_V2',
        'LNM_DISB_BATCH_COMPLETE_V2', 'LNM_COLLECTION_V2', 'LNM_COLLECTION_BATCH_V2',
        'FIN_SINGLE_ENTRY_V2', 'FIN_DOUBLE_ENTRY_V2', 'FIN_OFF_BALANCE_V2', 'FIN_TXN_CANCEL_V2',
        'FIN_CLOSING_V2', 'DPM_SETTLE_V2'
    )
);

UPDATE business_sla_task_policies SET step_code = 'UT_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE step_code = 'checker_review' AND sla_policy_id IN (
    SELECT id FROM business_sla_policies WHERE case_type IN (
        'LNM_DEBT_CHANGE_V2', 'LNM_RATE_CHANGE_V2', 'LNM_RESTRUCTURE_V2', 'LNM_WAIVER_V2',
        'LNM_WRITEOFF_V2', 'LNM_RECOVERY_V2', 'LNM_FUND_CHECK_V2', 'LNM_REVENUE_ALLOCATION_V2',
        'LNM_VFU_FEE_ALLOCATION_V2', 'LNM_OFF_BALANCE_EXPORT_V2',
        'LNM_DISB_REGISTER_V2', 'LNM_DISB_COMPLETE_V2', 'LNM_DISB_BATCH_REGISTER_V2',
        'LNM_DISB_BATCH_COMPLETE_V2', 'LNM_COLLECTION_V2', 'LNM_COLLECTION_BATCH_V2',
        'FIN_SINGLE_ENTRY_V2', 'FIN_DOUBLE_ENTRY_V2', 'FIN_OFF_BALANCE_V2', 'FIN_TXN_CANCEL_V2',
        'FIN_CLOSING_V2', 'DPM_SETTLE_V2'
    )
);

UPDATE business_sla_task_policies SET step_code = 'UT_TWRevalidate', updated_at = CURRENT_TIMESTAMP
WHERE sla_policy_id IN (SELECT id FROM business_sla_policies WHERE case_type = 'LOAN_FORMATION_V2')
  AND step_code = 'tw_revalidate';

UPDATE business_sla_task_policies SET step_code = 'UT_PGDReview', updated_at = CURRENT_TIMESTAMP
WHERE sla_policy_id IN (SELECT id FROM business_sla_policies WHERE case_type = 'LOAN_FORMATION_V2')
  AND step_code = 'pgd_review';

UPDATE business_sla_task_policies SET step_code = 'UT_GDReview', updated_at = CURRENT_TIMESTAMP
WHERE sla_policy_id IN (SELECT id FROM business_sla_policies WHERE case_type = 'LOAN_FORMATION_V2')
  AND step_code = 'gd_review';

UPDATE business_sla_task_policies SET step_code = 'UT_BoardReview', updated_at = CURRENT_TIMESTAMP
WHERE sla_policy_id IN (SELECT id FROM business_sla_policies WHERE case_type = 'LOAN_FORMATION_V2')
  AND step_code = 'board_review';

-- HRM v2 native roles: the BPMN now declares HRM_* candidate groups; the
-- catalog rows + assignment rules keep the resolver and workbench UI aligned.
INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('HRM_REGISTRATION_SUBMITTER', 'HRM submitter - lập hồ sơ nhân sự',      'MAKER',   'HRM', 'ACTIVE'),
    ('HRM_REGISTRATION_REVIEWER',  'HRM reviewer - phê duyệt hồ sơ nhân sự', 'CHECKER', 'HRM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_HRM_REG_V2_MAKER',   'HRM_EMPLOYEE_REGISTRATION', 'UT_MakerRevise',    'HRM_REGISTRATION_SUBMITTER', 'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_HRM_REG_V2_CHECKER', 'HRM_EMPLOYEE_REGISTRATION', 'UT_CheckerReview',  'HRM_REGISTRATION_REVIEWER',  'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_HRM_REG_V2_MAKER',   'HRM_EMPLOYEE_REGISTRATION', 'UT_MakerRevise',   'Chỉnh sửa hồ sơ nhân sự',   'HRM_REGISTRATION_SUBMITTER', 'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_HRM_REG_V2_CHECKER', 'HRM_EMPLOYEE_REGISTRATION', 'UT_CheckerReview', 'Phê duyệt hồ sơ nhân sự',  'HRM_REGISTRATION_REVIEWER',  'OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- RPT submit: built-in process without a case-type row (starting it failed)
-- and its BPMN carried LNM_* candidate groups — seed the missing pieces.
INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('RPT_MAKER',   'Report maker - hoàn thiện báo cáo',   'MAKER',   'RPT', 'ACTIVE'),
    ('RPT_CHECKER', 'Report checker - phê duyệt báo cáo',  'CHECKER', 'RPT', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, bpmn_version,
    workflow_enabled, maker_role, checker_role, owner_service, status
) VALUES
    ('RPT_SUBMIT_V2', 'STATISTICAL', 'Nộp báo cáo (v2)', 'rpt-submit-v2', 1, TRUE, 'RPT_MAKER', 'RPT_CHECKER', 'statistical-service', 'ACTIVE')
ON CONFLICT (case_type) DO UPDATE SET
    bpmn_process_id = EXCLUDED.bpmn_process_id, operation_name = EXCLUDED.operation_name,
    maker_role = EXCLUDED.maker_role, checker_role = EXCLUDED.checker_role,
    status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_RPT_SUBMIT_V2_MAKER',   'RPT_SUBMIT_V2', 'UT_MakerInput',    'RPT_MAKER',   'CANDIDATE_POOL', FALSE, '', 10, 'ACTIVE'),
    ('ASSIGN_RPT_SUBMIT_V2_CHECKER', 'RPT_SUBMIT_V2', 'UT_CheckerReview', 'RPT_CHECKER', 'CANDIDATE_POOL', TRUE,  '', 20, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

INSERT INTO business_process_roles (id, case_type, step_code, business_role, iam_role, action_scope, status) VALUES
    ('BPR_RPT_SUBMIT_V2_MAKER',   'RPT_SUBMIT_V2', 'UT_MakerInput',    'Hoàn thiện báo cáo', 'RPT_MAKER',   'OPEN,EDIT,SUBMIT', 'ACTIVE'),
    ('BPR_RPT_SUBMIT_V2_CHECKER', 'RPT_SUBMIT_V2', 'UT_CheckerReview', 'Phê duyệt báo cáo',  'RPT_CHECKER', 'OPEN,APPROVE',     'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_RPT_SUBMIT_V2_48H', 'SLA_RPT_SUBMIT_V2_48H', 'Nộp báo cáo 48h', 'RPT_SUBMIT_V2', 48, 8, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_RPT_SUBMIT_V2_MAKER',   'SLA_RPT_SUBMIT_V2_48H', 'UT_MakerInput',    'Hoàn thiện báo cáo', 8,  'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_RPT_SUBMIT_V2_CHECKER', 'SLA_RPT_SUBMIT_V2_48H', 'UT_CheckerReview', 'Phê duyệt báo cáo',  16, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- No down: rebuild mode.
SELECT 1;
