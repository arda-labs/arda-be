-- +goose Up
-- Saved AI model configurations per tenant (profiles) so admins can switch
-- providers without retyping credentials. Exactly one profile is active per
-- tenant; activating syncs the profile into ai_tenant_settings, which the
-- agent loop reads.
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

-- +goose Down
DROP TABLE IF EXISTS public.ai_tenant_setting_profiles;
