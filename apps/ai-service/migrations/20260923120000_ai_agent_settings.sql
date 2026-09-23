-- +goose Up
-- Per-tenant act-mode setting for the Olorin assistant. When enabled, the
-- assistant may execute confirm-kind tools at or below act_mode_max_risk
-- without a human approval — but only if the actor already holds the tool's
-- permission. High-risk actions always require approval. Default is disabled.
CREATE TABLE IF NOT EXISTS public.ai_agent_settings (
    tenant_id varchar(64) PRIMARY KEY,
    act_mode_enabled boolean NOT NULL DEFAULT false,
    act_mode_max_risk varchar(16) NOT NULL DEFAULT 'medium',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ai_agent_settings_max_risk_ck CHECK (act_mode_max_risk IN ('low', 'medium'))
);

-- +goose Down
DROP TABLE IF EXISTS public.ai_agent_settings;
