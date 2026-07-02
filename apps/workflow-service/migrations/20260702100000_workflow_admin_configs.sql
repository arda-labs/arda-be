-- +goose Up
CREATE TABLE business_sla_policies (
    id VARCHAR(64) PRIMARY KEY,
    code VARCHAR(100) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    case_type VARCHAR(100) NOT NULL REFERENCES business_operation_types(case_type),
    due_in_hours INTEGER NOT NULL,
    warning_in_hours INTEGER NOT NULL,
    escalation_role VARCHAR(100) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    effective_from TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    effective_to TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (due_in_hours > 0),
    CHECK (warning_in_hours >= 0),
    CHECK (warning_in_hours < due_in_hours)
);

CREATE TABLE business_sla_task_policies (
    id VARCHAR(64) PRIMARY KEY,
    sla_policy_id VARCHAR(64) NOT NULL REFERENCES business_sla_policies(id) ON DELETE CASCADE,
    step_code VARCHAR(100) NOT NULL,
    task_name VARCHAR(255) NOT NULL,
    duration_value INTEGER NOT NULL,
    duration_unit VARCHAR(20) NOT NULL,
    warning_mode VARCHAR(20) NOT NULL,
    warning_value INTEGER NOT NULL,
    warning_unit VARCHAR(20) NOT NULL,
    escalation_role VARCHAR(100) NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    effective_from TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    effective_to TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (duration_value > 0),
    CHECK (duration_unit IN ('MINUTE', 'HOUR')),
    CHECK (warning_mode IN ('ABSOLUTE', 'PERCENT')),
    CHECK (warning_value >= 0),
    CHECK (warning_unit IN ('MINUTE', 'HOUR', 'PERCENT')),
    UNIQUE(sla_policy_id, step_code)
);

CREATE TABLE business_description_templates (
    id VARCHAR(64) PRIMARY KEY,
    code VARCHAR(100) NOT NULL UNIQUE,
    case_type VARCHAR(100) NOT NULL REFERENCES business_operation_types(case_type),
    pattern TEXT NOT NULL,
    preview TEXT NOT NULL DEFAULT '',
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE business_process_roles (
    id VARCHAR(64) PRIMARY KEY,
    case_type VARCHAR(100) NOT NULL REFERENCES business_operation_types(case_type),
    step_code VARCHAR(100) NOT NULL,
    business_role VARCHAR(255) NOT NULL,
    iam_role VARCHAR(100) NOT NULL,
    action_scope TEXT NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(case_type, step_code, iam_role)
);

CREATE INDEX business_sla_policies_case_type_idx ON business_sla_policies(case_type);
CREATE INDEX business_sla_task_policies_sla_policy_id_idx ON business_sla_task_policies(sla_policy_id, sort_order);
CREATE INDEX business_description_templates_case_type_idx ON business_description_templates(case_type);
CREATE INDEX business_process_roles_case_type_idx ON business_process_roles(case_type);

INSERT INTO business_sla_policies (
    id, code, name, case_type, due_in_hours, warning_in_hours, escalation_role, status
) VALUES
    ('SLA_FIN_IN_8H', 'SLA_FIN_IN_8H', 'Incoming transaction same day', 'FINANCE_INCOMING_TRANSACTION', 8, 2, 'FINANCE_OPS_SUPERVISOR', 'ACTIVE'),
    ('SLA_FIN_OUT_8H', 'SLA_FIN_OUT_8H', 'Outgoing transaction same day', 'FINANCE_OUTGOING_TRANSACTION', 8, 2, 'FINANCE_OPS_SUPERVISOR', 'ACTIVE'),
    ('SLA_CUSTOMER_REG_24H', 'SLA_CUSTOMER_REG_24H', 'Customer registration 24h', 'CUSTOMER_REGISTRATION', 24, 4, 'CUSTOMER_SUPERVISOR', 'DRAFT');

INSERT INTO business_sla_task_policies (
    id, sla_policy_id, step_code, task_name, duration_value, duration_unit,
    warning_mode, warning_value, warning_unit, escalation_role, sort_order, status
) VALUES
    ('SLA_TASK_FIN_IN_CLASSIFY', 'SLA_FIN_IN_8H', 'classify-account', 'Classify account', 2, 'HOUR', 'ABSOLUTE', 30, 'MINUTE', 'FINANCE_OPS_SUPERVISOR', 10, 'ACTIVE'),
    ('SLA_TASK_FIN_IN_APPROVE', 'SLA_FIN_IN_8H', 'approve-journal', 'Approve journal', 4, 'HOUR', 'PERCENT', 75, 'PERCENT', 'FINANCE_OPS_SUPERVISOR', 20, 'ACTIVE'),
    ('SLA_TASK_FIN_OUT_VERIFY', 'SLA_FIN_OUT_8H', 'verify-beneficiary', 'Verify beneficiary', 2, 'HOUR', 'ABSOLUTE', 30, 'MINUTE', 'FINANCE_OPS_SUPERVISOR', 10, 'ACTIVE'),
    ('SLA_TASK_CUSTOMER_REG_REVIEW', 'SLA_CUSTOMER_REG_24H', 'review-registration', 'Review registration', 8, 'HOUR', 'PERCENT', 80, 'PERCENT', 'CUSTOMER_SUPERVISOR', 10, 'DRAFT');

UPDATE business_operation_types
SET default_sla_policy_id = 'SLA_FIN_IN_8H'
WHERE case_type = 'FINANCE_INCOMING_TRANSACTION';

UPDATE business_operation_types
SET default_sla_policy_id = 'SLA_FIN_OUT_8H'
WHERE case_type = 'FINANCE_OUTGOING_TRANSACTION';

UPDATE business_operation_types
SET default_sla_policy_id = 'SLA_CUSTOMER_REG_24H'
WHERE case_type = 'CUSTOMER_REGISTRATION';

INSERT INTO business_description_templates (
    id, code, case_type, pattern, preview, status
) VALUES
    ('DESC_FIN_IN', 'DESC_FIN_IN', 'FINANCE_INCOMING_TRANSACTION', '{caseCode} - {counterpartyName} - {amount} {currency}', 'FIN-IN-20260702-001 - Cong ty Minh An - 125.000.000 VND', 'ACTIVE'),
    ('DESC_FIN_OUT', 'DESC_FIN_OUT', 'FINANCE_OUTGOING_TRANSACTION', '{caseCode} - {beneficiaryName} - {amount} {currency}', 'FIN-OUT-20260702-004 - Nguyen Hoang Nam - 8.500.000 VND', 'ACTIVE'),
    ('DESC_CUSTOMER_REG', 'DESC_CUSTOMER_REG', 'CUSTOMER_REGISTRATION', '{caseCode} - {customerName} - {identityNo}', 'CUS-REG-20260702-009 - Nguyen Hoang Nam - 012345678901', 'DRAFT');

INSERT INTO business_process_roles (
    id, case_type, step_code, business_role, iam_role, action_scope, status
) VALUES
    ('ROLE_FIN_IN_MAKER', 'FINANCE_INCOMING_TRANSACTION', 'classify-account', 'Incoming transaction maker', 'FINANCE_TXN_MAKER', 'claim,save,submit', 'ACTIVE'),
    ('ROLE_FIN_IN_CHECKER', 'FINANCE_INCOMING_TRANSACTION', 'approve-journal', 'Journal checker', 'FINANCE_TXN_CHECKER', 'approve,reject,suspend', 'ACTIVE'),
    ('ROLE_FIN_OUT_MAKER', 'FINANCE_OUTGOING_TRANSACTION', 'verify-beneficiary', 'Outgoing transaction maker', 'FINANCE_TXN_MAKER', 'claim,save,submit', 'ACTIVE');

-- +goose Down
DROP TABLE IF EXISTS business_process_roles;
DROP TABLE IF EXISTS business_description_templates;
DROP TABLE IF EXISTS business_sla_task_policies;
DROP TABLE IF EXISTS business_sla_policies;
