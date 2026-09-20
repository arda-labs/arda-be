-- +goose Up

-- The decision dispatcher applies return/submit decisions to the owning domain
-- without a BPMN job context, so it must rebuild the tenant/org scope the
-- completing request carried. Record that scope on the decision row at
-- completion time (the CRM gRPC boundary rejects an unscoped call).
ALTER TABLE workflow_task_decisions ADD COLUMN IF NOT EXISTS tenant_id text;
ALTER TABLE workflow_task_decisions ADD COLUMN IF NOT EXISTS org_id text;

-- +goose Down

ALTER TABLE workflow_task_decisions DROP COLUMN IF EXISTS org_id;
ALTER TABLE workflow_task_decisions DROP COLUMN IF EXISTS tenant_id;
