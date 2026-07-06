-- +goose Up
-- CUSTOMER_REGISTRATION v2 uses UT_* native user tasks (not Activity_* service tasks).

UPDATE business_process_roles
SET step_code = 'UT_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_CheckerReview';

UPDATE business_process_roles
SET step_code = 'UT_MakerRevise', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_MakerRevise';

UPDATE workflow_assignment_rules
SET step_code = 'UT_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_CheckerReview';

UPDATE workflow_assignment_rules
SET step_code = 'UT_MakerRevise', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_MakerRevise';

UPDATE business_process_roles
SET status = 'INACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_RiskReview';

UPDATE workflow_assignment_rules
SET status = 'INACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_RiskReview';

-- +goose Down
UPDATE business_process_roles
SET step_code = 'Activity_CheckerReview', status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'UT_CheckerReview';

UPDATE business_process_roles
SET step_code = 'Activity_MakerRevise', status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'UT_MakerRevise';

UPDATE business_process_roles
SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_RiskReview';

UPDATE workflow_assignment_rules
SET step_code = 'Activity_CheckerReview', status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'UT_CheckerReview';

UPDATE workflow_assignment_rules
SET step_code = 'Activity_MakerRevise', status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'UT_MakerRevise';

UPDATE workflow_assignment_rules
SET status = 'ACTIVE', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION' AND step_code = 'Activity_RiskReview';
