-- +goose Up

-- Provider type is an allowlisted, server-owned transport preset. It is not
-- an arbitrary header bag: the runtime decides required headers and protocol.
ALTER TABLE public.ai_model_profiles
    ADD COLUMN IF NOT EXISTS provider_type VARCHAR(32) NOT NULL DEFAULT 'openai-compatible';

UPDATE public.ai_model_profiles
SET provider_type = 'opencode-go'
WHERE base_url LIKE 'https://opencode.ai/zen/go/v1%';

-- +goose Down

ALTER TABLE public.ai_model_profiles DROP COLUMN IF EXISTS provider_type;
