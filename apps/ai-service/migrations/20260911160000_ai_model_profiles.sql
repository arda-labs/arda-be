-- +goose Up

-- AI model profiles: a tenant can keep several named provider endpoints,
-- each holding many model IDs. Exactly one profile is applied (is_active)
-- and exactly one model within it is applied; the runtime resolves the
-- active profile + active model on every agent run.

CREATE TABLE IF NOT EXISTS public.ai_model_profiles (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    base_url    TEXT NOT NULL,
    api_key     TEXT NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_model_profiles_tenant_name_uk
    ON public.ai_model_profiles (tenant_id, name);
-- At most one applied profile per tenant.
CREATE UNIQUE INDEX IF NOT EXISTS ai_model_profiles_active_uk
    ON public.ai_model_profiles (tenant_id) WHERE is_active;

CREATE TABLE IF NOT EXISTS public.ai_profile_models (
    id          VARCHAR(64) PRIMARY KEY,
    profile_id  VARCHAR(64) NOT NULL REFERENCES public.ai_model_profiles(id) ON DELETE CASCADE,
    model_id    VARCHAR(128) NOT NULL,
    label       VARCHAR(128),
    is_active   BOOLEAN NOT NULL DEFAULT false,
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_profile_models_profile_model_uk
    ON public.ai_profile_models (profile_id, model_id);
-- At most one applied model per profile.
CREATE UNIQUE INDEX IF NOT EXISTS ai_profile_models_active_uk
    ON public.ai_profile_models (profile_id) WHERE is_active;

-- Backfill legacy single-model tenant settings into a "default" applied
-- profile so existing tenants keep working without re-configuration.
INSERT INTO public.ai_model_profiles (id, tenant_id, name, base_url, api_key, is_active, created_at, updated_at)
SELECT 'aip_' || left(md5(tenant_id), 24), tenant_id, 'default', base_url, api_key, true, created_at, updated_at
FROM public.ai_tenant_settings
WHERE is_active = true
ON CONFLICT DO NOTHING;

INSERT INTO public.ai_profile_models (id, profile_id, model_id, label, is_active, sort_order, created_at)
SELECT 'aim_' || left(md5(tenant_id || ':' || model_id), 24),
       'aip_' || left(md5(tenant_id), 24), model_id, NULL, true, 0, created_at
FROM public.ai_tenant_settings
WHERE is_active = true
ON CONFLICT DO NOTHING;

-- +goose Down

DROP TABLE IF EXISTS public.ai_profile_models;
DROP TABLE IF EXISTS public.ai_model_profiles;
