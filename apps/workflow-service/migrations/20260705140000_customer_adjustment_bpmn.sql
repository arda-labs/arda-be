-- +goose Up
UPDATE business_operation_types
SET bpmn_process_id = 'customer-adjustment-v1',
    operation_name = 'Điều chỉnh hồ sơ khách hàng',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT';

-- +goose Down
UPDATE business_operation_types
SET bpmn_process_id = 'customer-registration-v1',
    operation_name = 'Customer adjustment',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'CUSTOMER_ADJUSTMENT';
