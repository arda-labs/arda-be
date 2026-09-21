-- +goose Up

-- SLA org dimension (W3a). EPAS configures SLA per ORG_CODE × PROCESS_DEFINE_CODE
-- (com_cfg_sla_process_dtl / com_cfg_sla_task_dtl); Arda SLA was global per case
-- type. Policies now carry tenant_id/org_id; a row with empty org_id stays the
-- global fallback. The case keeps a single resolved sla_policy_id, so task
-- policies stay unchanged.

ALTER TABLE business_sla_policies
    ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS org_id VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS business_sla_policies_org_idx
    ON business_sla_policies (tenant_id, org_id, case_type);

-- Pilot tenant ngv-pilot (iam migration 20260921120000) × EPAS org 01 / 02.
-- Values from com_cfg_sla_process_dtl: org 01 max 100m warn 30m; org 02 max 90m
-- warn 80m. The case-level column is integer hours, so minutes round up
-- (org 02's 80m warning floors to 1h to satisfy warning < due).
INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role,
    status, tenant_id, org_id
) VALUES
    ('SLA_CUSTOMER_REG_O01', 'SLA_CUSTOMER_REG_O01', 'Đăng ký KH - Hội sở (EPAS 100m)', 'CUSTOMER_REGISTRATION', 2, 1, 'CUSTOMER_SUPERVISOR', 'ACTIVE', '00000000-0000-0000-0000-000000000020', '01'),
    ('SLA_CUSTOMER_ADJ_O01', 'SLA_CUSTOMER_ADJ_O01', 'Điều chỉnh KH - Hội sở (EPAS 100m)', 'CUSTOMER_ADJUSTMENT', 2, 1, 'CUSTOMER_SUPERVISOR', 'ACTIVE', '00000000-0000-0000-0000-000000000020', '01'),
    ('SLA_CUSTOMER_REG_O02', 'SLA_CUSTOMER_REG_O02', 'Đăng ký KH - Hà Nội (EPAS 90m)', 'CUSTOMER_REGISTRATION', 2, 1, 'CUSTOMER_SUPERVISOR', 'ACTIVE', '00000000-0000-0000-0000-000000000020', '02'),
    ('SLA_CUSTOMER_ADJ_O02', 'SLA_CUSTOMER_ADJ_O02', 'Điều chỉnh KH - Hà Nội (EPAS 90m)', 'CUSTOMER_ADJUSTMENT', 2, 1, 'CUSTOMER_SUPERVISOR', 'ACTIVE', '00000000-0000-0000-0000-000000000020', '02')
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    case_type = EXCLUDED.case_type,
    due_in_hours = EXCLUDED.due_in_hours,
    warning_in_hours = EXCLUDED.warning_in_hours,
    escalation_role = EXCLUDED.escalation_role,
    status = EXCLUDED.status,
    tenant_id = EXCLUDED.tenant_id,
    org_id = EXCLUDED.org_id,
    updated_at = CURRENT_TIMESTAMP;

-- Task policies mirror com_cfg_sla_task_dtl exactly (unit MINUTE).
--   org 01: checker 40m warn 15m, maker 60m warn 15m
--   org 02: checker 30m warn 25m, maker 60m warn 55m
INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_TASK_CUSTOMER_REG_O01_APPROVE', 'SLA_CUSTOMER_REG_O01', 'UT_CheckerReview', 'Phê duyệt hồ sơ khách hàng', 40, 'MINUTE', 'ABSOLUTE', 15, 'MINUTE', 'CUSTOMER_SUPERVISOR', 20, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_REG_O01_EDIT', 'SLA_CUSTOMER_REG_O01', 'UT_MakerRevise', 'Chỉnh sửa hồ sơ', 60, 'MINUTE', 'ABSOLUTE', 15, 'MINUTE', 'CUSTOMER_SUPERVISOR', 10, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_ADJ_O01_APPROVE', 'SLA_CUSTOMER_ADJ_O01', 'UT_CheckerReview', 'Phê duyệt điều chỉnh hồ sơ', 40, 'MINUTE', 'ABSOLUTE', 15, 'MINUTE', 'CUSTOMER_SUPERVISOR', 20, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_ADJ_O01_EDIT', 'SLA_CUSTOMER_ADJ_O01', 'UT_MakerRevise', 'Chỉnh sửa điều chỉnh hồ sơ', 60, 'MINUTE', 'ABSOLUTE', 15, 'MINUTE', 'CUSTOMER_SUPERVISOR', 10, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_REG_O02_APPROVE', 'SLA_CUSTOMER_REG_O02', 'UT_CheckerReview', 'Phê duyệt hồ sơ khách hàng', 30, 'MINUTE', 'ABSOLUTE', 25, 'MINUTE', 'CUSTOMER_SUPERVISOR', 20, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_REG_O02_EDIT', 'SLA_CUSTOMER_REG_O02', 'UT_MakerRevise', 'Chỉnh sửa hồ sơ', 60, 'MINUTE', 'ABSOLUTE', 55, 'MINUTE', 'CUSTOMER_SUPERVISOR', 10, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_ADJ_O02_APPROVE', 'SLA_CUSTOMER_ADJ_O02', 'UT_CheckerReview', 'Phê duyệt điều chỉnh hồ sơ', 30, 'MINUTE', 'ABSOLUTE', 25, 'MINUTE', 'CUSTOMER_SUPERVISOR', 20, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_ADJ_O02_EDIT', 'SLA_CUSTOMER_ADJ_O02', 'UT_MakerRevise', 'Chỉnh sửa điều chỉnh hồ sơ', 60, 'MINUTE', 'ABSOLUTE', 55, 'MINUTE', 'CUSTOMER_SUPERVISOR', 10, 'ACTIVE')
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

-- +goose Down

DELETE FROM business_sla_task_policies
WHERE id IN (
    'SLA_TASK_CUSTOMER_REG_O01_APPROVE', 'SLA_TASK_CUSTOMER_REG_O01_EDIT',
    'SLA_TASK_CUSTOMER_ADJ_O01_APPROVE', 'SLA_TASK_CUSTOMER_ADJ_O01_EDIT',
    'SLA_TASK_CUSTOMER_REG_O02_APPROVE', 'SLA_TASK_CUSTOMER_REG_O02_EDIT',
    'SLA_TASK_CUSTOMER_ADJ_O02_APPROVE', 'SLA_TASK_CUSTOMER_ADJ_O02_EDIT'
);

DELETE FROM business_sla_policies
WHERE id IN ('SLA_CUSTOMER_REG_O01', 'SLA_CUSTOMER_ADJ_O01', 'SLA_CUSTOMER_REG_O02', 'SLA_CUSTOMER_ADJ_O02');

DROP INDEX IF EXISTS business_sla_policies_org_idx;
ALTER TABLE business_sla_policies
    DROP COLUMN IF EXISTS org_id,
    DROP COLUMN IF EXISTS tenant_id;
