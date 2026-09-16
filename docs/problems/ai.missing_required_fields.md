---
code: ai.missing_required_fields
status: 400
title: Missing required fields
summary: |
  A model settings request decoded fine but lacks mandatory values: profiles
  need `name`, `modelId`, and a usable API key.
client_action: |
  Fill in the missing fields and resend. On profile create, an empty or masked
  API key falls back to the tenant's current key — this code means no usable
  key was found either.
operator_action: |
  Trace `request_id` and compare the payload with the OpenAPI schema for
  `POST /api/ai/settings/profiles`; base URL problems return
  `ai.invalid_base_url`/`ai.base_url_not_allowed` instead.
---

Emitted by the profile create handler after URL validation, so it is purely
about absent required values.
