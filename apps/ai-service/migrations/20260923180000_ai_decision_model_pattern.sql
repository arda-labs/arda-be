-- +goose Up
-- A0a: decision models are no longer limited to a fixed two-entry allowlist.
-- Tenants may configure any System One model ID; the shape is validated here
-- and by internal/decision.Settings.Valid, and the settings connectivity test
-- is the operator's verification step.
ALTER TABLE public.ai_decision_settings
    DROP CONSTRAINT ai_decision_settings_model_id_check;
ALTER TABLE public.ai_decision_settings
    ADD CONSTRAINT ai_decision_settings_model_id_check
    CHECK (model_id ~ '^[A-Za-z0-9][A-Za-z0-9._:/_-]{0,127}$');

-- +goose Down
ALTER TABLE public.ai_decision_settings
    DROP CONSTRAINT ai_decision_settings_model_id_check;
ALTER TABLE public.ai_decision_settings
    ADD CONSTRAINT ai_decision_settings_model_id_check
    CHECK (model_id IN ('jev-1.13-free', 'jev-1.13'));
