-- +goose Up
CREATE TABLE IF NOT EXISTS public.ai_tenant_settings (
    tenant_id VARCHAR(64) PRIMARY KEY,
    provider_type VARCHAR(64) NOT NULL DEFAULT 'openai',
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    model_id VARCHAR(128) NOT NULL,
    temperature REAL NOT NULL DEFAULT 0.2,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS public.ai_tenant_settings;
