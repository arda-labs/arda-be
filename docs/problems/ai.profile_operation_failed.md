---
code: ai.profile_operation_failed
status: 400
title: Profile operation failed
summary: |
  The profile request was rejected because of invalid input or a persistence
  error that is not covered by a more specific problem code.
client_action: |
  Check the request body: `name`, `baseUrl` and `apiKey` are required on
  create; Model IDs must be non-empty; a profile must own at least one model.
  Retry once after fixing the payload; do not retry unchanged.
operator_action: |
  Trace `request_id` and inspect the ai-service logs for the underlying error
  (validation vs. database), then check recent migrations on
  `ai_model_profiles` / `ai_profile_models`.
---

Emitted by profile create/update/models handlers when input validation fails
or the store returns an unexpected error that has no dedicated code.
