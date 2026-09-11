---
code: ai.profile_endpoint_not_found
status: 404
title: Profile endpoint not found
summary: |
  The URL under `/api/ai/settings/profiles/{id}` is not a known action.
client_action: |
  Do not retry. Use one of the supported routes: `{id}` (GET/PUT/DELETE),
  `{id}/models` (POST), `{id}/models/{modelId}` (DELETE), `{id}/apply` (POST),
  `{id}/test` (POST).
operator_action: |
  Trace `request_id` and confirm the client is on the latest AI console build;
  unknown sub-actions usually indicate a stale frontend.
---

Emitted when the path segment after the profile id is not a recognized action.
