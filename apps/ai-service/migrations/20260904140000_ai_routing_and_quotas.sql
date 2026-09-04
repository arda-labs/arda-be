-- +goose Up
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

CREATE TABLE IF NOT EXISTS public.ai_tenant_quota_settings (
    tenant_id VARCHAR(64) PRIMARY KEY,
    webhook_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS public.ai_tenant_quota_settings;
DROP TABLE IF EXISTS public.ai_department_budgets;
DROP TABLE IF EXISTS public.ai_tenant_routing_rules;
