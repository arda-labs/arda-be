-- +goose Up
UPDATE business_operation_types
SET bpmn_process_id = 'crm-customer-registration-v2',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION';

-- +goose Down
UPDATE business_operation_types
SET bpmn_process_id = 'customer-registration-v1',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_REGISTRATION';
