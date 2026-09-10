---
code: ai.profile_not_found
status: 404
title: Profile not found
summary: |
  No model profile exists for this tenant under the given id, or the profiles
  URL is not a valid action path (`{id}` for delete, `{id}/activate`).
client_action: |
  Do not retry unchanged. Verify the profile id against
  `GET /api/ai/settings/profiles` and refresh stale views; profiles are
  tenant-scoped.
operator_action: |
  Trace `request_id`, confirm the profile exists in the ai-service database
  for the tenant, and check tenant scoping of the lookup.
---

Emitted by `DELETE /api/ai/settings/profiles/{id}` and
`POST /api/ai/settings/profiles/{id}/activate` for unknown ids, over-long ids,
and unknown sub-actions. A profile that exists but cannot be saved returns
`ai.profile_delete_failed`/`ai.profile_activate_failed` instead.
