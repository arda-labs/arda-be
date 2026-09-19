-- +goose Up

-- Task activations (P1): one workflow_tasks row per engine user-task
-- activation instead of one row per (case, step). Return loops and retries
-- keep their history; the projector binds the eager ROUTING row for the
-- first activation and inserts a new activation for every later one.
--
-- Verified against production data 2026-09-19: no duplicate
-- (case_id, step_code) pairs existed, so the swap is safe.

ALTER TABLE workflow_tasks
    DROP CONSTRAINT IF EXISTS workflow_tasks_case_id_task_type_step_code_key;

CREATE UNIQUE INDEX IF NOT EXISTS workflow_tasks_activation_uq
    ON workflow_tasks (case_id, step_code, activation_no);

-- +goose Down

DROP INDEX IF EXISTS workflow_tasks_activation_uq;

ALTER TABLE workflow_tasks
    ADD CONSTRAINT workflow_tasks_case_id_task_type_step_code_key
    UNIQUE (case_id, task_type, step_code);
