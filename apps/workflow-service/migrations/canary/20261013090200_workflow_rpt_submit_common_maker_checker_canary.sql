-- +goose Up

-- ADR-011 wave 2: route RPT_SUBMIT_V2 to the shared common-maker-checker
-- process. The mc.* jobs then reach statistical-service's CaseCommandService
-- (kind rpt.submit) instead of the legacy rpt-submit-v2 workers.
-- Apply manually, and only after the workflow-service and statistical-service images
-- that serve CaseCommandService are running. Startup reads the active routes,
-- so restart workflow-service after this script so it dials statistical-service.
UPDATE business_operation_types
SET bpmn_process_id = 'common-maker-checker',
    worker_kind = 'rpt.submit',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('RPT_SUBMIT_V2')
  AND status = 'ACTIVE'
  AND owner_service = 'statistical-service'
  AND bpmn_process_id = 'rpt-submit-v2'
  AND (worker_kind IS NULL OR worker_kind = 'rpt.submit')
  AND NULLIF(BTRIM(maker_role), '') IS NOT NULL
  AND NULLIF(BTRIM(checker_role), '') IS NOT NULL;

-- DRAFT cases have no process instance yet, so they follow new cases.
-- In-flight cases keep their pinned rpt-submit-v2 instance.
UPDATE business_cases
SET bpmn_process_id = 'common-maker-checker',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('RPT_SUBMIT_V2')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'rpt-submit-v2';

-- +goose StatementBegin
DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM business_operation_types
        WHERE case_type IN ('RPT_SUBMIT_V2')
          AND status = 'ACTIVE'
          AND bpmn_process_id = 'common-maker-checker'
          AND worker_kind = 'rpt.submit'
    ) <> 1 THEN
        RAISE EXCEPTION 'RPT_SUBMIT_V2 common-maker-checker canary did not apply (check owner_service and maker/checker roles)';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Roll back only after every non-terminal RPT_SUBMIT_V2 case on the common
-- process has drained. Never switch a live common-process instance.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business_cases
        WHERE case_type IN ('RPT_SUBMIT_V2')
          AND bpmn_process_id = 'common-maker-checker'
          AND status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
    ) THEN
        RAISE EXCEPTION 'cannot roll back RPT_SUBMIT_V2 canary while common-process cases are active';
    END IF;
END
$$;
-- +goose StatementEnd

UPDATE business_cases
SET bpmn_process_id = 'rpt-submit-v2',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('RPT_SUBMIT_V2')
  AND status = 'DRAFT'
  AND bpmn_process_id = 'common-maker-checker';

UPDATE business_operation_types
SET bpmn_process_id = 'rpt-submit-v2',
    worker_kind = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type IN ('RPT_SUBMIT_V2')
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'common-maker-checker'
  AND worker_kind = 'rpt.submit';
