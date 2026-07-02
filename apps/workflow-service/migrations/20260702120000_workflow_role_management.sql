-- +goose Up
CREATE TABLE workflow_role_catalog (
    role_code VARCHAR(100) PRIMARY KEY,
    role_name VARCHAR(255) NOT NULL,
    role_type VARCHAR(30) NOT NULL,
    business_subsystem VARCHAR(20) NOT NULL DEFAULT 'FAC',
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE workflow_role_memberships (
    id VARCHAR(64) PRIMARY KEY,
    role_code VARCHAR(100) NOT NULL REFERENCES workflow_role_catalog(role_code),
    principal_type VARCHAR(20) NOT NULL,
    principal_id VARCHAR(100) NOT NULL,
    tenant_id VARCHAR(64) NOT NULL DEFAULT '',
    org_id VARCHAR(64) NOT NULL DEFAULT '',
    branch_id VARCHAR(64) NOT NULL DEFAULT '',
    product_code VARCHAR(100) NOT NULL DEFAULT '',
    min_amount NUMERIC(20, 2),
    max_amount NUMERIC(20, 2),
    effective_from TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    effective_to TIMESTAMP WITH TIME ZONE,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE workflow_assignment_rules (
    id VARCHAR(64) PRIMARY KEY,
    case_type VARCHAR(100) NOT NULL REFERENCES business_operation_types(case_type),
    step_code VARCHAR(100) NOT NULL,
    role_code VARCHAR(100) NOT NULL REFERENCES workflow_role_catalog(role_code),
    assignment_mode VARCHAR(30) NOT NULL,
    require_separation_of_duties BOOLEAN NOT NULL DEFAULT TRUE,
    fallback_role_code VARCHAR(100) NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 100,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(case_type, step_code, role_code)
);

CREATE TABLE workflow_delegations (
    id VARCHAR(64) PRIMARY KEY,
    from_principal_id VARCHAR(100) NOT NULL,
    to_principal_id VARCHAR(100) NOT NULL,
    role_code VARCHAR(100) NOT NULL REFERENCES workflow_role_catalog(role_code),
    effective_from TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    effective_to TIMESTAMP WITH TIME ZONE,
    reason TEXT NOT NULL DEFAULT '',
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX workflow_role_memberships_role_code_idx ON workflow_role_memberships(role_code);
CREATE INDEX workflow_role_memberships_principal_idx ON workflow_role_memberships(principal_type, principal_id);
CREATE INDEX workflow_assignment_rules_case_step_idx ON workflow_assignment_rules(case_type, step_code, priority);
CREATE INDEX workflow_delegations_role_code_idx ON workflow_delegations(role_code);
CREATE INDEX workflow_delegations_principal_idx ON workflow_delegations(from_principal_id, to_principal_id);

INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('FINANCE_TXN_MAKER', 'Finance transaction maker', 'MAKER', 'FAC', 'ACTIVE'),
    ('FINANCE_TXN_CHECKER', 'Finance transaction checker', 'CHECKER', 'FAC', 'ACTIVE'),
    ('FINANCE_OPS_SUPERVISOR', 'Finance operations supervisor', 'SUPERVISOR', 'FAC', 'ACTIVE'),
    ('CUSTOMER_MAKER', 'Customer maker', 'MAKER', 'CRM', 'ACTIVE'),
    ('CUSTOMER_CHECKER', 'Customer checker', 'CHECKER', 'CRM', 'ACTIVE'),
    ('CUSTOMER_SUPERVISOR', 'Customer supervisor', 'SUPERVISOR', 'CRM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_FIN_IN_CLASSIFY', 'FINANCE_INCOMING_TRANSACTION', 'classify-account', 'FINANCE_TXN_MAKER', 'CANDIDATE_POOL', FALSE, 'FINANCE_OPS_SUPERVISOR', 10, 'ACTIVE'),
    ('ASSIGN_FIN_IN_APPROVE', 'FINANCE_INCOMING_TRANSACTION', 'approve-journal', 'FINANCE_TXN_CHECKER', 'CANDIDATE_POOL', TRUE, 'FINANCE_OPS_SUPERVISOR', 20, 'ACTIVE'),
    ('ASSIGN_FIN_OUT_VERIFY', 'FINANCE_OUTGOING_TRANSACTION', 'verify-beneficiary', 'FINANCE_TXN_MAKER', 'CANDIDATE_POOL', FALSE, 'FINANCE_OPS_SUPERVISOR', 10, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS workflow_delegations;
DROP TABLE IF EXISTS workflow_assignment_rules;
DROP TABLE IF EXISTS workflow_role_memberships;
DROP TABLE IF EXISTS workflow_role_catalog;
