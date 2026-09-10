-- +goose Up

-- HRM v2 SLA: the employee-registration native flow had no SLA policy
-- (v1 was job-based). Task rows key on the BPMN element ids (UT_*), matching
-- the resolver/projector step_code contract aligned in 20260910130000.

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_HRM_REG_V2_48H', 'SLA_HRM_REG_V2_48H', 'Đăng ký nhân sự 48h', 'HRM_EMPLOYEE_REGISTRATION', 48, 8, '', 'ACTIVE')
ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_HRM_REG_V2_MAKER',   'SLA_HRM_REG_V2_48H', 'UT_MakerRevise',   'Chỉnh sửa hồ sơ nhân sự',  8,  'HOUR', 'PERCENT', 75, 'PERCENT', '', 10, 'ACTIVE'),
    ('SLA_HRM_REG_V2_CHECKER', 'SLA_HRM_REG_V2_48H', 'UT_CheckerReview', 'Phê duyệt hồ sơ nhân sự',  16, 'HOUR', 'PERCENT', 75, 'PERCENT', '', 20, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM business_sla_task_policies WHERE sla_policy_id = 'SLA_HRM_REG_V2_48H';
DELETE FROM business_sla_policies WHERE id = 'SLA_HRM_REG_V2_48H';
