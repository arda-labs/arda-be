-- +goose Up

-- System One (Jev) is served both by TypeSafe AI directly and through the
-- OpenCode Zen gateway on the same wire protocol. Existing rows keep Zen.
ALTER TABLE public.ai_decision_settings
    ADD COLUMN IF NOT EXISTS provider VARCHAR(32) NOT NULL DEFAULT 'opencode-zen';

ALTER TABLE public.ai_decision_settings
    ADD CONSTRAINT ai_decision_settings_provider_check
        CHECK (provider IN ('opencode-zen', 'typesafe'));

-- +goose Down

ALTER TABLE public.ai_decision_settings
    DROP CONSTRAINT IF EXISTS ai_decision_settings_provider_check;

ALTER TABLE public.ai_decision_settings
    DROP COLUMN IF EXISTS provider;
