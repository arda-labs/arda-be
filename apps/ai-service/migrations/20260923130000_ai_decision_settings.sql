-- +goose Up
CREATE TABLE public.ai_decision_settings (
    tenant_id VARCHAR(64) PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    model_id VARCHAR(128) NOT NULL DEFAULT 'jev-1.13-free'
        CHECK (model_id IN ('jev-1.13-free', 'jev-1.13')),
    min_confidence DOUBLE PRECISION NOT NULL DEFAULT 0.8
        CHECK (min_confidence >= 0.5 AND min_confidence <= 1),
    api_key TEXT NOT NULL DEFAULT '' CHECK (api_key = '' OR api_key LIKE 'enc:v1:%'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (NOT enabled OR api_key <> '')
);

ALTER TABLE public.ai_runs ADD COLUMN decision_usage JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE public.ai_runs DROP COLUMN decision_usage;
DROP TABLE public.ai_decision_settings;
