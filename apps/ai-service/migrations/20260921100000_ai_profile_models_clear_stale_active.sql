-- +goose Up
-- Normalize stale applied-model flags. ApplyProfileModel used to clear the
-- active model only within the profile being applied, so every other profile
-- kept a leftover is_active=true model. The runtime resolves the active config
-- through the active profile, so those leftovers were inert but made the AI
-- Settings UI advertise models as "in use" on profiles that were not applied.
-- Only one model may be applied per tenant; clear the rest.
UPDATE public.ai_profile_models m
SET is_active = false
FROM public.ai_model_profiles p
WHERE m.profile_id = p.id
  AND m.is_active = true
  AND p.is_active = false;

-- +goose Down
-- Intentionally irreversible: this removes stale derived state, and the
-- per-profile leftover flags cannot be reconstructed meaningfully.
SELECT 1;
