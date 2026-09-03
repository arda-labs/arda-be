-- +goose Up
CREATE TABLE IF NOT EXISTS public.ai_agents (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    department VARCHAR(64) NOT NULL,
    description TEXT,
    system_prompt TEXT NOT NULL,
    model_id VARCHAR(128) NOT NULL,
    temperature REAL NOT NULL DEFAULT 0.2,
    allowed_tools TEXT[] NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_agents_tenant_name_uq UNIQUE (tenant_id, name)
);

-- +goose Down
DROP TABLE IF EXISTS public.ai_agents;
