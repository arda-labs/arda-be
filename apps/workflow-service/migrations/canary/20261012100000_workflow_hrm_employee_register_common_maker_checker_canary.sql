-- +goose Up

-- ADR-011 pilot: route HRM_EMPLOYEE_REGISTRATION to the shared
-- common-maker-checker process. The mc.* jobs then reach hrm-service's
-- CaseCommandService instead of the hrm.employee.register.* workers.
-- Apply manually, and only after the workflow-service and hrm-service images
-- that serve CaseCommandService are running. Startup reads the active routes,
-- so restart workflow-service after this script so it dials hrm-service.
UPDATE business_operation_types
SET bpmn_process_id = 'common-maker-checker',
    worker_kind = 'hrm.employee-register',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION'
  AND status = 'ACTIVE'
  AND owner_service = 'hrm-service'
  AND bpmn_process_id = 'hrm-employee-registration-v2'
  AND (worker_kind IS NULL OR worker_kind = 'hrm.employee-register')
  AND NULLIF(BTRIM(maker_role), '') IS NOT NULL
  AND NULLIF(BTRIM(checker_role), '') IS NOT NULL;

-- DRAFT cases have no process instance yet, so they follow new cases.
-- In-flight cases keep their pinned hrm-employee-registration-v2 instance.
UPDATE business_cases
SET bpmn_process_id = 'common-maker-checker',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION'
  AND status = 'DRAFT'
  AND bpmn_process_id = 'hrm-employee-registration-v2';

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM business_operation_types
        WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION'
          AND status = 'ACTIVE'
          AND bpmn_process_id = 'common-maker-checker'
          AND worker_kind = 'hrm.employee-register'
    ) THEN
        RAISE EXCEPTION 'HRM_EMPLOYEE_REGISTRATION common-maker-checker canary did not apply (check owner_service and maker/checker roles)';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Roll back only after every non-terminal HRM_EMPLOYEE_REGISTRATION case on
-- the common process has drained. Never switch a live common-process instance.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business_cases
        WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION'
          AND bpmn_process_id = 'common-maker-checker'
          AND status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
    ) THEN
        RAISE EXCEPTION 'cannot roll back HRM_EMPLOYEE_REGISTRATION canary while common-process cases are active';
    END IF;
END
$$;
-- +goose StatementEnd

UPDATE business_cases
SET bpmn_process_id = 'hrm-employee-registration-v2',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION'
  AND status = 'DRAFT'
  AND bpmn_process_id = 'common-maker-checker';

UPDATE business_operation_types
SET bpmn_process_id = 'hrm-employee-registration-v2',
    worker_kind = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION'
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'common-maker-checker'
  AND worker_kind = 'hrm.employee-register';
