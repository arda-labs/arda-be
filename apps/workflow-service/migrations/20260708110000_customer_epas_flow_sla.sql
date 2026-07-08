-- +goose Up
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_CUSTOMER_REG_EPAS_24H', 'SLA_CUSTOMER_REG_EPAS_24H', 'Customer registration EPAS flow 24h', 'CUSTOMER_REGISTRATION', 24, 4, 'CUSTOMER_SUPERVISOR', 'ACTIVE'),
    ('SLA_CUSTOMER_ADJ_EPAS_24H', 'SLA_CUSTOMER_ADJ_EPAS_24H', 'Customer adjustment EPAS flow 24h', 'CUSTOMER_ADJUSTMENT', 24, 4, 'CUSTOMER_SUPERVISOR', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    case_type = EXCLUDED.case_type,
    due_in_hours = EXCLUDED.due_in_hours,
    warning_in_hours = EXCLUDED.warning_in_hours,
    escalation_role = EXCLUDED.escalation_role,
    status = EXCLUDED.status,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_TASK_CUSTOMER_REG_EDIT', 'SLA_CUSTOMER_REG_EPAS_24H', 'UT_MakerRevise', 'Chỉnh sửa hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'CUSTOMER_SUPERVISOR', 10, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_REG_APPROVE', 'SLA_CUSTOMER_REG_EPAS_24H', 'UT_CheckerReview', 'Phê duyệt hồ sơ khách hàng', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'CUSTOMER_SUPERVISOR', 20, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_ADJ_EDIT', 'SLA_CUSTOMER_ADJ_EPAS_24H', 'UT_MakerRevise', 'Chỉnh sửa điều chỉnh hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'CUSTOMER_SUPERVISOR', 10, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_ADJ_APPROVE', 'SLA_CUSTOMER_ADJ_EPAS_24H', 'UT_CheckerReview', 'Phê duyệt điều chỉnh hồ sơ', 8, 'HOUR', 'PERCENT', 75, 'PERCENT', 'CUSTOMER_SUPERVISOR', 20, 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET
    sla_policy_id = EXCLUDED.sla_policy_id,
    step_code = EXCLUDED.step_code,
    task_name = EXCLUDED.task_name,
    duration_value = EXCLUDED.duration_value,
    duration_unit = EXCLUDED.duration_unit,
    warning_mode = EXCLUDED.warning_mode,
    warning_value = EXCLUDED.warning_value,
    warning_unit = EXCLUDED.warning_unit,
    escalation_role = EXCLUDED.escalation_role,
    sort_order = EXCLUDED.sort_order,
    status = EXCLUDED.status,
    updated_at = CURRENT_TIMESTAMP;

UPDATE business_operation_types
SET default_sla_policy_id = 'SLA_CUSTOMER_REG_EPAS_24H',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION';

UPDATE business_operation_types
SET default_sla_policy_id = 'SLA_CUSTOMER_ADJ_EPAS_24H',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT';

-- +goose Down
UPDATE business_operation_types
SET default_sla_policy_id = 'SLA_CUSTOMER_REG_24H',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION';

UPDATE business_operation_types
SET default_sla_policy_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT';

DELETE FROM business_sla_task_policies
WHERE id IN (
    'SLA_TASK_CUSTOMER_REG_EDIT',
    'SLA_TASK_CUSTOMER_REG_APPROVE',
    'SLA_TASK_CUSTOMER_ADJ_EDIT',
    'SLA_TASK_CUSTOMER_ADJ_APPROVE'
);

DELETE FROM business_sla_policies
WHERE id IN (
    'SLA_CUSTOMER_REG_EPAS_24H',
    'SLA_CUSTOMER_ADJ_EPAS_24H'
);
