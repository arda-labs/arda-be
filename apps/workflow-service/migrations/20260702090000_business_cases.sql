-- +goose Up
CREATE TABLE business_operation_types (
    case_type VARCHAR(100) PRIMARY KEY,
    business_area VARCHAR(100) NOT NULL,
    operation_name VARCHAR(255) NOT NULL,
    bpmn_process_id VARCHAR(255) NOT NULL,
    bpmn_version INTEGER NOT NULL DEFAULT 1,
    workflow_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    default_sla_policy_id VARCHAR(100),
    maker_role VARCHAR(100) NOT NULL,
    checker_role VARCHAR(100) NOT NULL,
    owner_service VARCHAR(100) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    effective_from TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    effective_to TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE business_cases (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    case_type VARCHAR(100) NOT NULL REFERENCES business_operation_types(case_type),
    case_code VARCHAR(80) NOT NULL UNIQUE,
    title VARCHAR(255) NOT NULL,
    primary_object_type VARCHAR(100) NOT NULL,
    primary_object_id VARCHAR(100) NOT NULL,
    domain_service VARCHAR(100) NOT NULL,
    status VARCHAR(30) NOT NULL,
    current_step VARCHAR(100) NOT NULL DEFAULT '',
    priority VARCHAR(20) NOT NULL DEFAULT 'NORMAL',
    created_by VARCHAR(100) NOT NULL,
    assigned_to VARCHAR(100),
    candidate_role VARCHAR(100),
    sla_policy_id VARCHAR(100),
    sla_due_at TIMESTAMP WITH TIME ZONE,
    process_instance_key BIGINT,
    bpmn_process_id VARCHAR(255),
    bpmn_version INTEGER,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE case_timeline_events (
    id BIGSERIAL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL REFERENCES business_cases(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    from_status VARCHAR(30),
    to_status VARCHAR(30),
    actor VARCHAR(100),
    note TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX business_cases_case_type_idx ON business_cases(case_type);
CREATE INDEX business_cases_status_idx ON business_cases(status);
CREATE INDEX business_cases_assigned_to_idx ON business_cases(assigned_to);
CREATE INDEX business_cases_candidate_role_idx ON business_cases(candidate_role);
CREATE INDEX business_cases_updated_at_idx ON business_cases(updated_at DESC);
CREATE INDEX case_timeline_events_case_id_idx ON case_timeline_events(case_id, created_at DESC);

INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, owner_service, maker_role, checker_role
) VALUES
    ('CUSTOMER_REGISTRATION', 'CUSTOMER', 'Customer registration', 'customer-registration-v1', 'crm-service', 'CUSTOMER_MAKER', 'CUSTOMER_CHECKER'),
    ('CUSTOMER_RISK_REVIEW', 'CUSTOMER', 'Customer risk review', 'customer-risk-review-v1', 'crm-service', 'CUSTOMER_RISK_MAKER', 'CUSTOMER_RISK_CHECKER'),
    ('FINANCE_INCOMING_TRANSACTION', 'FINANCE', 'Incoming transaction', 'finance-incoming-transaction-v1', 'finance-service', 'FINANCE_TXN_MAKER', 'FINANCE_TXN_CHECKER'),
    ('FINANCE_OUTGOING_TRANSACTION', 'FINANCE', 'Outgoing transaction', 'finance-outgoing-transaction-v1', 'finance-service', 'FINANCE_TXN_MAKER', 'FINANCE_TXN_CHECKER');

-- +goose Down
DROP TABLE IF EXISTS case_timeline_events;
DROP TABLE IF EXISTS business_cases;
DROP TABLE IF EXISTS business_operation_types;
