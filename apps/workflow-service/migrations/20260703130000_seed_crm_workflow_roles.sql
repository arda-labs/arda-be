-- +goose Up
INSERT INTO workflow_role_catalog (role_code, role_name, role_type, business_subsystem, status) VALUES
    ('CUSTOMER_RISK_CHECKER', 'Customer risk checker', 'CHECKER', 'CRM', 'ACTIVE')
ON CONFLICT (role_code) DO NOTHING;

UPDATE business_sla_policies
SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE id = 'SLA_CUSTOMER_REG_24H';

UPDATE business_sla_task_policies
SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE id = 'SLA_TASK_CUSTOMER_REG_REVIEW';

UPDATE business_description_templates
SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE id = 'DESC_CUSTOMER_REG';

INSERT INTO business_process_roles (
    id, case_type, step_code, business_role, iam_role, action_scope, status
) VALUES
    ('ROLE_CUSTOMER_CHECKER_REVIEW', 'CUSTOMER_REGISTRATION', 'Activity_CheckerReview', 'Customer checker review', 'CUSTOMER_CHECKER', 'approve,request_changes,reject', 'ACTIVE'),
    ('ROLE_CUSTOMER_MAKER_REVISE', 'CUSTOMER_REGISTRATION', 'Activity_MakerRevise', 'Customer maker revise', 'CUSTOMER_MAKER', 'save,submit', 'ACTIVE'),
    ('ROLE_CUSTOMER_RISK_REVIEW', 'CUSTOMER_REGISTRATION', 'Activity_RiskReview', 'Customer risk review', 'CUSTOMER_RISK_CHECKER', 'approve,request_changes,reject', 'ACTIVE')
ON CONFLICT (case_type, step_code, iam_role) DO NOTHING;

INSERT INTO workflow_assignment_rules (
    id, case_type, step_code, role_code, assignment_mode,
    require_separation_of_duties, fallback_role_code, priority, status
) VALUES
    ('ASSIGN_CUSTOMER_CHECKER_REVIEW', 'CUSTOMER_REGISTRATION', 'Activity_CheckerReview', 'CUSTOMER_CHECKER', 'CANDIDATE_POOL', TRUE, 'CUSTOMER_SUPERVISOR', 10, 'ACTIVE'),
    ('ASSIGN_CUSTOMER_MAKER_REVISE', 'CUSTOMER_REGISTRATION', 'Activity_MakerRevise', 'CUSTOMER_MAKER', 'DIRECT', FALSE, 'CUSTOMER_SUPERVISOR', 20, 'ACTIVE'),
    ('ASSIGN_CUSTOMER_RISK_REVIEW', 'CUSTOMER_REGISTRATION', 'Activity_RiskReview', 'CUSTOMER_RISK_CHECKER', 'CANDIDATE_POOL', TRUE, 'CUSTOMER_SUPERVISOR', 30, 'ACTIVE')
ON CONFLICT (case_type, step_code, role_code) DO NOTHING;

-- +goose Down
DELETE FROM workflow_assignment_rules
WHERE id IN (
    'ASSIGN_CUSTOMER_CHECKER_REVIEW',
    'ASSIGN_CUSTOMER_MAKER_REVISE',
    'ASSIGN_CUSTOMER_RISK_REVIEW'
);

DELETE FROM business_process_roles
WHERE id IN (
    'ROLE_CUSTOMER_CHECKER_REVIEW',
    'ROLE_CUSTOMER_MAKER_REVISE',
    'ROLE_CUSTOMER_RISK_REVIEW'
);

DELETE FROM workflow_role_catalog
WHERE role_code = 'CUSTOMER_RISK_CHECKER';
