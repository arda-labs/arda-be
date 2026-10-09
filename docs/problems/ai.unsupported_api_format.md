---
code: ai.unsupported_api_format
status: 400
title: Unsupported API format
summary: |
  The requested model API format is not one of the wire protocols ai-service
  can speak.
client_action: |
  Select `chat_completions`, `anthropic_messages`, `openai_responses` or
  `google_gemini` and submit the profile again. Do not retry the unchanged request.
operator_action: |
  Trace `request_id` and verify that the deployed ai-service version and the
  migration adding `api_format` are current.
---

Emitted by `POST /api/ai/settings/profiles` and
`PUT /api/ai/settings/profiles/{id}`, `PATCH
/api/ai/settings/profiles/{id}/models/{modelId}` and
`POST /api/ai/settings/profiles/{id}/models` when an API format is not
recognised.
