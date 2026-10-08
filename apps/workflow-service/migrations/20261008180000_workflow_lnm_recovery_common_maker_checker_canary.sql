-- +goose Up

-- Canary LNM_RECOVERY_V2 only after workflow-service has deployed
-- common-maker-checker and registered all mc.* handlers.
UPDATE business_operation_types
SET bpmn_process_id = 'common-maker-checker',
    worker_kind = 'lnm.recovery',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_RECOVERY_V2'
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'lnm-recovery-v2'
  AND (worker_kind IS NULL OR worker_kind = 'lnm.recovery');

-- Existing DRAFT cases have no started process instance, so point them at the
-- same process as new cases. In-flight cases keep their pinned old definition.
UPDATE business_cases
SET bpmn_process_id = 'common-maker-checker',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_RECOVERY_V2'
  AND status = 'DRAFT'
  AND bpmn_process_id = 'lnm-recovery-v2';

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM business_operation_types
        WHERE case_type = 'LNM_RECOVERY_V2'
          AND status = 'ACTIVE'
          AND bpmn_process_id = 'common-maker-checker'
          AND worker_kind = 'lnm.recovery'
    ) THEN
        RAISE EXCEPTION 'LNM_RECOVERY_V2 common-maker-checker canary did not apply';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Rollback only after every non-terminal LNM_RECOVERY_V2 case using the
-- common process has drained. Never switch a live common-process instance.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business_cases
        WHERE case_type = 'LNM_RECOVERY_V2'
          AND bpmn_process_id = 'common-maker-checker'
          AND status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
    ) THEN
        RAISE EXCEPTION 'cannot roll back LNM_RECOVERY_V2 canary while common-process cases are active';
    END IF;
END
$$;
-- +goose StatementEnd

UPDATE business_cases
SET bpmn_process_id = 'lnm-recovery-v2',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_RECOVERY_V2'
  AND status = 'DRAFT'
  AND bpmn_process_id = 'common-maker-checker';

UPDATE business_operation_types
SET bpmn_process_id = 'lnm-recovery-v2',
    worker_kind = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_RECOVERY_V2'
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'common-maker-checker'
  AND worker_kind = 'lnm.recovery';
