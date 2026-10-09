---
code: ai.unsupported_reasoning_effort
status: 400
title: Unsupported reasoning effort
summary: |
  The requested reasoning effort is not one of the supported levels.
client_action: |
  Send an empty string (provider default), `low`, `medium` or `high` and
  submit the profile again. Do not retry the unchanged request.
operator_action: |
  Trace `request_id` and verify that the deployed ai-service version and the
  migration adding `reasoning_effort` are current.
---

Emitted by `POST /api/ai/settings/profiles` and
`PUT /api/ai/settings/profiles/{id}` when `reasoningEffort` is not recognised.
