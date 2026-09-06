-- +goose Up

-- P1c.5: repoint HRM employee registration to the native-userTask v2
-- process (same shape as crm-customer-registration-v2) + job workers.

UPDATE business_operation_types
SET bpmn_process_id = 'hrm-employee-registration-v2',
    bpmn_version = 2,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION';

-- +goose Down
SELECT 1;
