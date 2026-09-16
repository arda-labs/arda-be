---
code: ai.unsupported_provider_type
status: 400
title: Unsupported provider type
summary: |
  The requested AI provider preset is not supported by the current ai-service
  transport adapters.
client_action: |
  Select one of the supported presets (`openai`, `openai-compatible`,
  `opencode-go`, `ollama`, or `vllm`) and submit the profile again. Do not
  retry the unchanged request.
operator_action: |
  Trace `request_id` and verify that the deployed ai-service version and
  migration containing `provider_type` are current.
---

Emitted by `POST /api/ai/settings/profiles` and
`PUT /api/ai/settings/profiles/{id}` when the provider preset is unknown.
