---
code: ai.model_not_found
status: 404
title: Model not found
summary: |
  The requested Model ID does not belong to the target profile, so it cannot be
  applied or deleted.
client_action: |
  Do not retry unchanged. Reload `GET /api/ai/settings/profiles` and pick a
  Model ID that is listed under the profile.
operator_action: |
  Trace `request_id`, confirm the row exists in `ai_profile_models` for the
  profile, and check that the model id matches exactly (case-sensitive).
---

Emitted by `POST /api/ai/settings/profiles/{id}/apply` (model not in profile)
and `DELETE /api/ai/settings/profiles/{id}/models/{modelId}` (unknown model).
