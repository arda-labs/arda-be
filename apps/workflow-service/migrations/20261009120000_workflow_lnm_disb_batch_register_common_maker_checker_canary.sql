-- +goose Up

-- Canary LNM_DISB_BATCH_REGISTER_V2 after the workflow-service image with
-- common-maker-checker and the lnm.disb-batch-register mc.* adapter is live.
UPDATE business_operation_types
SET bpmn_process_id = 'common-maker-checker',
    worker_kind = 'lnm.disb-batch-register',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_DISB_BATCH_REGISTER_V2'
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'lnm-disb-batch-register-v2'
  AND (worker_kind IS NULL OR worker_kind = 'lnm.disb-batch-register');

-- DRAFT cases have no started Zeebe instance; point them at the canary process
-- so a later submit cannot start the retired maker-input-first path.
UPDATE business_cases
SET bpmn_process_id = 'common-maker-checker',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_DISB_BATCH_REGISTER_V2'
  AND status = 'DRAFT'
  AND bpmn_process_id = 'lnm-disb-batch-register-v2';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM business_operation_types
        WHERE case_type = 'LNM_DISB_BATCH_REGISTER_V2'
          AND status = 'ACTIVE'
          AND bpmn_process_id = 'common-maker-checker'
          AND worker_kind = 'lnm.disb-batch-register'
    ) THEN
        RAISE EXCEPTION 'LNM_DISB_BATCH_REGISTER_V2 common-maker-checker canary did not apply';
    END IF;
END
$$;

-- Roll back only after all non-terminal cases using common-maker-checker
-- have drained; never repoint a live Zeebe process instance.
--
-- +goose Down

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business_cases
        WHERE case_type = 'LNM_DISB_BATCH_REGISTER_V2'
          AND bpmn_process_id = 'common-maker-checker'
          AND status NOT IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REJECTED')
    ) THEN
        RAISE EXCEPTION 'cannot roll back batch-register canary while common-process cases are active';
    END IF;
END
$$;

UPDATE business_cases
SET bpmn_process_id = 'lnm-disb-batch-register-v2',
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_DISB_BATCH_REGISTER_V2'
  AND status = 'DRAFT'
  AND bpmn_process_id = 'common-maker-checker';

UPDATE business_operation_types
SET bpmn_process_id = 'lnm-disb-batch-register-v2',
    worker_kind = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE case_type = 'LNM_DISB_BATCH_REGISTER_V2'
  AND status = 'ACTIVE'
  AND bpmn_process_id = 'common-maker-checker'
  AND worker_kind = 'lnm.disb-batch-register';
