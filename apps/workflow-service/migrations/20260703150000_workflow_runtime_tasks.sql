-- +goose Up
CREATE TABLE workflow_tasks (
    id VARCHAR(64) PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL REFERENCES business_cases(id) ON DELETE CASCADE,
    process_instance_key BIGINT,
    job_key BIGINT,
    task_type VARCHAR(160) NOT NULL,
    step_code VARCHAR(160) NOT NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(30) NOT NULL DEFAULT 'READY',
    candidate_role VARCHAR(100) NOT NULL DEFAULT '',
    candidate_group_id VARCHAR(100) NOT NULL DEFAULT '',
    candidate_org_unit_id VARCHAR(100) NOT NULL DEFAULT '',
    assigned_to VARCHAR(100) NOT NULL DEFAULT '',
    assigned_at TIMESTAMP WITH TIME ZONE,
    claim_expires_at TIMESTAMP WITH TIME ZONE,
    sla_due_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(case_id, task_type, step_code)
);

CREATE INDEX workflow_tasks_case_id_idx ON workflow_tasks(case_id);
CREATE INDEX workflow_tasks_status_idx ON workflow_tasks(status, updated_at DESC);
CREATE INDEX workflow_tasks_candidate_role_idx ON workflow_tasks(candidate_role);
CREATE INDEX workflow_tasks_assigned_to_idx ON workflow_tasks(assigned_to);
CREATE INDEX workflow_tasks_sla_due_at_idx ON workflow_tasks(sla_due_at);

-- +goose Down
DROP TABLE IF EXISTS workflow_tasks;
