-- +goose Up

-- A profile now chooses the wire protocol of its provider endpoint instead of
-- assuming OpenAI chat-completions, and may ask reasoning models for a given
-- effort or an explicit thinking-token budget. Existing profiles keep chat
-- completions and the provider default, so nothing changes for them until an
-- admin edits the profile.
ALTER TABLE public.ai_model_profiles
    ADD COLUMN IF NOT EXISTS api_format VARCHAR(32) NOT NULL DEFAULT 'chat_completions',
    ADD COLUMN IF NOT EXISTS reasoning_effort VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reasoning_budget_tokens INTEGER NOT NULL DEFAULT 0;

ALTER TABLE public.ai_model_profiles
    ADD CONSTRAINT ai_model_profiles_api_format_check
        CHECK (api_format IN ('chat_completions', 'anthropic_messages', 'openai_responses', 'google_gemini')),
    ADD CONSTRAINT ai_model_profiles_reasoning_effort_check
        CHECK (reasoning_effort IN ('', 'low', 'medium', 'high')),
    ADD CONSTRAINT ai_model_profiles_reasoning_budget_check
        CHECK (reasoning_budget_tokens = 0 OR reasoning_budget_tokens BETWEEN 1024 AND 64000);

-- One gateway key can serve several protocols (OpenCode Zen routes Claude,
-- GPT-5.x and DeepSeek on different paths), so a model may override the
-- profile's format. NULL inherits the profile.
ALTER TABLE public.ai_profile_models
    ADD COLUMN IF NOT EXISTS api_format VARCHAR(32);

ALTER TABLE public.ai_profile_models
    ADD CONSTRAINT ai_profile_models_api_format_check
        CHECK (api_format IS NULL OR api_format IN ('chat_completions', 'anthropic_messages', 'openai_responses', 'google_gemini'));

-- +goose Down

ALTER TABLE public.ai_profile_models
    DROP CONSTRAINT IF EXISTS ai_profile_models_api_format_check;

ALTER TABLE public.ai_profile_models
    DROP COLUMN IF EXISTS api_format;

ALTER TABLE public.ai_model_profiles
    DROP CONSTRAINT IF EXISTS ai_model_profiles_reasoning_budget_check,
    DROP CONSTRAINT IF EXISTS ai_model_profiles_reasoning_effort_check,
    DROP CONSTRAINT IF EXISTS ai_model_profiles_api_format_check;

ALTER TABLE public.ai_model_profiles
    DROP COLUMN IF EXISTS reasoning_budget_tokens,
    DROP COLUMN IF EXISTS reasoning_effort,
    DROP COLUMN IF EXISTS api_format;
