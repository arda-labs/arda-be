-- +goose Up

-- ADR-011 wave 2: route CFC_CONTRACT_V1 to the shared common-maker-checker
-- process. The mc.* jobs then reach capital-service's CaseCommandService
-- (kind cfc.contract) instead of the legacy cfc-contract-v1 workers.
-- Apply manually, and only after the workflow-service and capital-service images
-- that serve CaseCommandService are running. Startup reads the active routes,
-- so restart workflow-service after this script so it dials capital-service.
UPDATE business_operation_types
SET bpmn_process_id = 'common-maker-checker',
    worker_kind = 'cfc.contract',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('CFC_CONTRACT_V1')
  AND status = 'ACTIVE'
  AND owner_service = 'capital-service'
  AND bpmn_process_id = 'cfc-contract-v1'
  AND (worker_kind IS NULL OR worker_kind = 'cfc.contract')
  AND NULLIF(BTRIM(maker_role), '') IS NOT NULL
  AND NULLIF(BTRIM(checker_role), '') IS NOT NULL;

-- DRAFT cases have no process instance yet, so they follow new cases.
-- In-flight cases keep their pinned cfc-contract-v1 instance.
UPDATE business_cases
SET bpmn_process_id = 'common-maker-checker',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('CFC_CONTRACT_V1')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'cfc-contract-v1';

-- +goose StatementBegin
DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM business_operation_types
        WHERE case_type IN ('CFC_CONTRACT_V1')
          AND status = 'ACTIVE'
          AND bpmn_process_id = 'common-maker-checker'
          AND worker_kind = 'cfc.contract'
    ) <> 1 THEN
        RAISE EXCEPTION 'CFC_CONTRACT_V1 common-maker-checker canary did not apply (check owner_service and maker/checker roles)';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Roll back only after every non-terminal CFC_CONTRACT_V1 case on the common
-- process has drained. Never switch a live common-process instance.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business_cases
        WHERE case_type IN ('CFC_CONTRACT_V1')
          AND bpmn_process_id = 'common-maker-checker'
          AND status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
    ) THEN
        RAISE EXCEPTION 'cannot roll back CFC_CONTRACT_V1 canary while common-process cases are active';
    END IF;
END
$$;
-- +goose StatementEnd

UPDATE business_cases
SET bpmn_process_id = 'cfc-contract-v1',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('CFC_CONTRACT_V1')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'common-maker-checker';

UPDATE business_operation_types
SET bpmn_process_id = 'cfc-contract-v1',
    worker_kind = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('CFC_CONTRACT_V1')
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'common-maker-checker'
  AND worker_kind = 'cfc.contract';
