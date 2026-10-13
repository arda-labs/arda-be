-- +goose Up

-- ADR-011 wave 2: route DPM_BATCH_INTEREST_V1 to the shared common-maker-checker
-- process. The mc.* jobs then reach deposit-service's CaseCommandService
-- (kind dpm.interest-batch) instead of the legacy dpm-interest-v1 workers. The batch case lists its ops by case id.
-- Apply manually, and only after the workflow-service and deposit-service images
-- that serve CaseCommandService are running. Startup reads the active routes,
-- so restart workflow-service after this script so it dials deposit-service.
UPDATE business_operation_types
SET bpmn_process_id = 'common-maker-checker',
    worker_kind = 'dpm.interest-batch',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_BATCH_INTEREST_V1')
  AND status = 'ACTIVE'
  AND owner_service = 'deposit-service'
  AND bpmn_process_id = 'dpm-interest-v1'
  AND (worker_kind IS NULL OR worker_kind = 'dpm.interest-batch')
  AND NULLIF(BTRIM(maker_role), '') IS NOT NULL
  AND NULLIF(BTRIM(checker_role), '') IS NOT NULL;

-- DRAFT cases have no process instance yet, so they follow new cases.
-- In-flight cases keep their pinned dpm-interest-v1 instance.
UPDATE business_cases
SET bpmn_process_id = 'common-maker-checker',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_BATCH_INTEREST_V1')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'dpm-interest-v1';

-- +goose StatementBegin
DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM business_operation_types
        WHERE case_type IN ('DPM_BATCH_INTEREST_V1')
          AND status = 'ACTIVE'
          AND bpmn_process_id = 'common-maker-checker'
          AND worker_kind = 'dpm.interest-batch'
    ) <> 1 THEN
        RAISE EXCEPTION 'DPM_BATCH_INTEREST_V1 common-maker-checker canary did not apply (check owner_service and maker/checker roles)';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Roll back only after every non-terminal DPM_BATCH_INTEREST_V1 case on the common
-- process has drained. Never switch a live common-process instance.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business_cases
        WHERE case_type IN ('DPM_BATCH_INTEREST_V1')
          AND bpmn_process_id = 'common-maker-checker'
          AND status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
    ) THEN
        RAISE EXCEPTION 'cannot roll back DPM_BATCH_INTEREST_V1 canary while common-process cases are active';
    END IF;
END
$$;
-- +goose StatementEnd

UPDATE business_cases
SET bpmn_process_id = 'dpm-interest-v1',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_BATCH_INTEREST_V1')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'common-maker-checker';

UPDATE business_operation_types
SET bpmn_process_id = 'dpm-interest-v1',
    worker_kind = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_BATCH_INTEREST_V1')
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'common-maker-checker'
  AND worker_kind = 'dpm.interest-batch';
