-- +goose Up
INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id,
    maker_role, checker_role, owner_service, status
)
VALUES (
    'CUSTOMER_ADJUSTMENT',
    'CUSTOMER',
    'Customer adjustment',
    'customer-adjustment-v1',
    'CUSTOMER_MAKER',
    'CUSTOMER_CHECKER',
    'crm-service',
    'ACTIVE'
)
ON CONFLICT (case_type) DO UPDATE SET
    operation_name = EXCLUDED.operation_name,
    bpmn_process_id = EXCLUDED.bpmn_process_id,
    maker_role = EXCLUDED.maker_role,
    checker_role = EXCLUDED.checker_role,
    owner_service = EXCLUDED.owner_service,
    status = EXCLUDED.status,
    updated_at = CURRENT_TIMESTAMP;

-- +goose Down
DELETE FROM business_operation_types WHERE case_type = 'CUSTOMER_ADJUSTMENT';
