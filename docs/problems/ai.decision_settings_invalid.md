---
code: ai.decision_settings_invalid
status: 400
title: Invalid decision model settings
summary: |
  The decision model must be Jev 1.13 Free or Jev 1.13, with a confidence
  threshold between 0.5 and 1. Enabling or testing requires a credential.
client_action: |
  Check the model, confidence threshold and OpenCode Zen API key. Omit api_key
  to preserve the saved key; an empty value clears it and requires enabled=false.
operator_action: |
  Check the request schema. Encrypted values and masked placeholders are not
  accepted as new API keys. Never log the submitted key.
related_routes:
  - PUT /api/ai/settings/decision
  - POST /api/ai/settings/decision/test
---
