-- +goose Up

-- Workflow registry v2 (P0, additive only — no behavior change).
-- Case-type step metadata becomes the single source of truth for discovery,
-- allowed actions and form keys; cases pin the registry version they started
-- with so metadata edits never rewrite an in-flight dossier.
--
-- Runtime switches to reading these tables in P1 (projector/decision pipeline).

ALTER TABLE business_operation_types
    ADD COLUMN IF NOT EXISTS runtime VARCHAR(40) NOT NULL DEFAULT 'NATIVE_USER_TASK',
    ADD COLUMN IF NOT EXISTS registry_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS discovery_mode VARCHAR(40) NOT NULL DEFAULT 'REGISTRY';

ALTER TABLE business_cases
    ADD COLUMN IF NOT EXISTS registry_version INTEGER;

CREATE TABLE IF NOT EXISTS workflow_case_type_steps (
    id VARCHAR(64) PRIMARY KEY,
    case_type VARCHAR(100) NOT NULL REFERENCES business_operation_types(case_type),
    registry_version INTEGER NOT NULL,
    element_id VARCHAR(160) NOT NULL,
    step_code VARCHAR(160) NOT NULL,
    step_kind VARCHAR(30) NOT NULL,
    form_key VARCHAR(200) NOT NULL,
    allowed_actions TEXT[] NOT NULL DEFAULT '{}',
    required_comment_on TEXT[] NOT NULL DEFAULT '{}',
    assignment_rule_id VARCHAR(64),
    sla_policy_id VARCHAR(100),
    data_contract VARCHAR(160),
    sort_order INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (case_type, registry_version, element_id)
);

CREATE INDEX IF NOT EXISTS workflow_case_type_steps_lookup_idx
    ON workflow_case_type_steps (case_type, registry_version, status);

-- Per-activation task lifecycle: activation_no keeps one row per engine user
-- task activation (return loops no longer overwrite history); engine_state
-- mirrors the Zeebe user task state for claim/expiry reconciliation.
ALTER TABLE workflow_tasks
    ADD COLUMN IF NOT EXISTS activation_no INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS engine_state VARCHAR(30) NOT NULL DEFAULT 'CREATED',
    ADD COLUMN IF NOT EXISTS engine_checked_at TIMESTAMP WITH TIME ZONE;

-- Durable decision log: "decision received" (workflow) is recorded before the
-- engine/domain side effects; the dispatcher/reconciler moves it to APPLIED.
CREATE TABLE IF NOT EXISTS workflow_task_decisions (
    id VARCHAR(64) PRIMARY KEY,
    task_id VARCHAR(64) NOT NULL REFERENCES workflow_tasks(id) ON DELETE CASCADE,
    case_id VARCHAR(64) NOT NULL REFERENCES business_cases(id) ON DELETE CASCADE,
    process_instance_key BIGINT NOT NULL,
    element_id VARCHAR(160) NOT NULL,
    decision VARCHAR(30) NOT NULL,
    comment TEXT NOT NULL DEFAULT '',
    actor VARCHAR(100) NOT NULL,
    data_version VARCHAR(160),
    status VARCHAR(30) NOT NULL DEFAULT 'RECORDED',
    idempotency_key VARCHAR(200) NOT NULL,
    recorded_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    dispatched_at TIMESTAMP WITH TIME ZONE,
    applied_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS workflow_task_decisions_reconcile_idx
    ON workflow_task_decisions (status, recorded_at);

-- +goose Down

DROP TABLE IF EXISTS workflow_task_decisions;
ALTER TABLE workflow_tasks
    DROP COLUMN IF EXISTS engine_checked_at,
    DROP COLUMN IF EXISTS engine_state,
    DROP COLUMN IF EXISTS activation_no;
DROP TABLE IF EXISTS workflow_case_type_steps;
ALTER TABLE business_cases DROP COLUMN IF EXISTS registry_version;
ALTER TABLE business_operation_types
    DROP COLUMN IF EXISTS discovery_mode,
    DROP COLUMN IF EXISTS registry_version,
    DROP COLUMN IF EXISTS runtime;
