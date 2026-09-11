---
code: ai.profile_name_taken
status: 409
title: Profile name already taken
summary: |
  A model profile with the same name already exists for this tenant. Profile
  names are unique per tenant.
client_action: |
  Do not retry unchanged. Rename the profile or edit the existing one after
  reloading `GET /api/ai/settings/profiles`.
operator_action: |
  Trace `request_id`, inspect `ai_model_profiles` for the tenant, and check for
  concurrent writers or a stale client that skipped the uniqueness check.
---

Emitted by `POST /api/ai/settings/profiles` and
`PUT /api/ai/settings/profiles/{id}` on the unique `(tenant_id, name)` index.
