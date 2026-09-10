-- +goose Up
-- AI Settings stays tenant-owned (one active model config per tenant), but the
-- redundant layers around it are removed: saved profiles, model routing rules
-- and USD department budgets. The unused temperature knob is dropped in favour
-- of provider defaults.
ALTER TABLE public.ai_tenant_settings DROP COLUMN IF EXISTS temperature;
DROP TABLE IF EXISTS public.ai_department_budgets;
DROP TABLE IF EXISTS public.ai_tenant_routing_rules;
DROP TABLE IF EXISTS public.ai_tenant_setting_profiles;

-- +goose Down
ALTER TABLE public.ai_tenant_settings ADD COLUMN IF NOT EXISTS temperature REAL NOT NULL DEFAULT 0.2;

CREATE TABLE IF NOT EXISTS public.ai_tenant_setting_profiles (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    provider_type VARCHAR(64) NOT NULL DEFAULT 'openai',
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    model_id VARCHAR(128) NOT NULL,
    temperature REAL NOT NULL DEFAULT 0.2,
    is_active BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_tenant_setting_profiles_name_uq UNIQUE (tenant_id, name)
);

CREATE UNIQUE INDEX IF NOT EXISTS ai_tenant_setting_profiles_active_uq
    ON public.ai_tenant_setting_profiles (tenant_id) WHERE is_active;

CREATE TABLE IF NOT EXISTS public.ai_tenant_routing_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(64) NOT NULL,
    fast_model VARCHAR(128) NOT NULL DEFAULT 'gemini-2.5-flash',
    code_model VARCHAR(128) NOT NULL DEFAULT 'claude-3.5-sonnet',
    sensitive_model VARCHAR(128) NOT NULL DEFAULT 'qwen2.5:7b-instruct-q4_K_M',
    primary_provider VARCHAR(64) NOT NULL DEFAULT 'gemini',
    secondary_provider VARCHAR(64) NOT NULL DEFAULT 'openai',
    failover_provider VARCHAR(64) NOT NULL DEFAULT 'ollama',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_tenant_routing_rules_tenant_uq UNIQUE (tenant_id)
);

CREATE TABLE IF NOT EXISTS public.ai_department_budgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(64) NOT NULL,
    department VARCHAR(64) NOT NULL,
    monthly_limit NUMERIC(10, 2) NOT NULL DEFAULT 100.00,
    spent NUMERIC(10, 4) NOT NULL DEFAULT 0.0000,
    rpm_limit INT NOT NULL DEFAULT 60,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_dept_budgets_uq UNIQUE (tenant_id, department)
);
