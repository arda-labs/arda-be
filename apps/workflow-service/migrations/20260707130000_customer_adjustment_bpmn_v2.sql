-- +goose Up
UPDATE business_operation_types
SET bpmn_process_id = 'customer-adjustment-v2',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT';

UPDATE business_process_roles
SET step_code = 'UT_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'Activity_CheckerReview';

UPDATE business_process_roles
SET step_code = 'UT_MakerRevise', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'Activity_MakerRevise';

UPDATE workflow_assignment_rules
SET step_code = 'UT_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'Activity_CheckerReview';

UPDATE workflow_assignment_rules
SET step_code = 'UT_MakerRevise', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'Activity_MakerRevise';

-- +goose Down
UPDATE business_operation_types
SET bpmn_process_id = 'customer-adjustment-v1',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT';

UPDATE business_process_roles
SET step_code = 'Activity_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'UT_CheckerReview';

UPDATE business_process_roles
SET step_code = 'Activity_MakerRevise', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'UT_MakerRevise';

UPDATE workflow_assignment_rules
SET step_code = 'Activity_CheckerReview', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'UT_CheckerReview';

UPDATE workflow_assignment_rules
SET step_code = 'Activity_MakerRevise', updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT' AND step_code = 'UT_MakerRevise';
