-- +goose Up

-- ADR-011 wave 2: route DPM_SETTLE_V2 to the shared common-maker-checker
-- process. The mc.* jobs then reach deposit-service's CaseCommandService
-- (kind dpm.settle) instead of the legacy dpm-settle-v2 workers.
-- Apply manually, and only after the workflow-service and deposit-service images
-- that serve CaseCommandService are running. Startup reads the active routes,
-- so restart workflow-service after this script so it dials deposit-service.
UPDATE business_operation_types
SET bpmn_process_id = 'common-maker-checker',
    worker_kind = 'dpm.settle',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_SETTLE_V2')
  AND status = 'ACTIVE'
  AND owner_service = 'deposit-service'
  AND bpmn_process_id = 'dpm-settle-v2'
  AND (worker_kind IS NULL OR worker_kind = 'dpm.settle')
  AND NULLIF(BTRIM(maker_role), '') IS NOT NULL
  AND NULLIF(BTRIM(checker_role), '') IS NOT NULL;

-- DRAFT cases have no process instance yet, so they follow new cases.
-- In-flight cases keep their pinned dpm-settle-v2 instance.
UPDATE business_cases
SET bpmn_process_id = 'common-maker-checker',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_SETTLE_V2')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'dpm-settle-v2';

-- +goose StatementBegin
DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM business_operation_types
        WHERE case_type IN ('DPM_SETTLE_V2')
          AND status = 'ACTIVE'
          AND bpmn_process_id = 'common-maker-checker'
          AND worker_kind = 'dpm.settle'
    ) <> 1 THEN
        RAISE EXCEPTION 'DPM_SETTLE_V2 common-maker-checker canary did not apply (check owner_service and maker/checker roles)';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Roll back only after every non-terminal DPM_SETTLE_V2 case on the common
-- process has drained. Never switch a live common-process instance.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business_cases
        WHERE case_type IN ('DPM_SETTLE_V2')
          AND bpmn_process_id = 'common-maker-checker'
          AND status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
    ) THEN
        RAISE EXCEPTION 'cannot roll back DPM_SETTLE_V2 canary while common-process cases are active';
    END IF;
END
$$;
-- +goose StatementEnd

UPDATE business_cases
SET bpmn_process_id = 'dpm-settle-v2',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_SETTLE_V2')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'common-maker-checker';

UPDATE business_operation_types
SET bpmn_process_id = 'dpm-settle-v2',
    worker_kind = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('DPM_SETTLE_V2')
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'common-maker-checker'
  AND worker_kind = 'dpm.settle';
