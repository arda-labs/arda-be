-- +goose Up
INSERT INTO business_operation_types (
    case_type, business_area, operation_name, bpmn_process_id, owner_service, maker_role, checker_role, status
)
VALUES (
    'HRM_EMPLOYEE_REGISTRATION',
    'HRM',
    'Employee registration',
    'hrm-employee-registration-v1',
    'hrm-service',
    'HRM_REGISTRATION_SUBMITTER',
    'HRM_REGISTRATION_REVIEWER',
    'ACTIVE'
)
ON CONFLICT (case_type) DO UPDATE SET
    business_area = EXCLUDED.business_area,
    operation_name = EXCLUDED.operation_name,
    bpmn_process_id = EXCLUDED.bpmn_process_id,
    owner_service = EXCLUDED.owner_service,
    maker_role = EXCLUDED.maker_role,
    checker_role = EXCLUDED.checker_role,
    status = EXCLUDED.status,
    updated_at = now();

-- +goose Down
DELETE FROM business_operation_types
WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION';
