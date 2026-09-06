-- +goose Up

-- Candidate pool visibility: assignment-rule resolution (Known Gap #2) now
-- computes the concrete user pool per task step and persists it alongside the
-- role so the workbench can show who a task is waiting on.
ALTER TABLE workflow_tasks
    ADD COLUMN IF NOT EXISTS candidate_users TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE workflow_tasks
    DROP COLUMN IF EXISTS candidate_users;
