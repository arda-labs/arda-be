-- +goose Up

-- W7-lite: HRM registration SLA seed + retire the 3 legacy case types whose
-- BPMN v1 files are no longer in the corpus (open question #7 in
-- epas-to-arda-matrix.md).

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_HRM_EMPLOYEE_REGISTRATION_24H', 'SLA_HRM_EMPLOYEE_REGISTRATION_24H',
     'Đăng ký nhân viên 24h', 'HRM_EMPLOYEE_REGISTRATION', 24, 4, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_HRM_REG_MAKER',   'SLA_HRM_EMPLOYEE_REGISTRATION_24H', 'UT_MakerInput',    'Nhập hồ sơ nhân viên',  4, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_HRM_REG_CHECKER', 'SLA_HRM_EMPLOYEE_REGISTRATION_24H', 'UT_CheckerReview', 'Phê duyệt hồ sơ nhân viên', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

UPDATE business_operation_types
SET status = 'RETIRED', updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('FINANCE_INCOMING_TRANSACTION', 'FINANCE_OUTGOING_TRANSACTION', 'CUSTOMER_RISK_REVIEW');

UPDATE workflow_assignment_rules SET status = 'RETIRED', updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('FINANCE_INCOMING_TRANSACTION', 'FINANCE_OUTGOING_TRANSACTION', 'CUSTOMER_RISK_REVIEW');

UPDATE business_sla_policies SET status = 'RETIRED', updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('FINANCE_INCOMING_TRANSACTION', 'FINANCE_OUTGOING_TRANSACTION', 'CUSTOMER_RISK_REVIEW');

-- +goose Down

UPDATE business_operation_types
SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('FINANCE_INCOMING_TRANSACTION', 'FINANCE_OUTGOING_TRANSACTION', 'CUSTOMER_RISK_REVIEW');

UPDATE workflow_assignment_rules SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('FINANCE_INCOMING_TRANSACTION', 'FINANCE_OUTGOING_TRANSACTION', 'CUSTOMER_RISK_REVIEW');

UPDATE business_sla_policies SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('FINANCE_INCOMING_TRANSACTION', 'FINANCE_OUTGOING_TRANSACTION', 'CUSTOMER_RISK_REVIEW');

DELETE FROM business_sla_task_policies WHERE id IN ('SLA_HRM_REG_MAKER', 'SLA_HRM_REG_CHECKER');
DELETE FROM business_sla_policies WHERE id = 'SLA_HRM_EMPLOYEE_REGISTRATION_24H';
