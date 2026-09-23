---
code: ai.model_purpose_mismatch
status: 400
title: Model purpose mismatch
summary: |
  Jev is a structured decision model and cannot be configured as a chat model.
client_action: |
  Configure Jev in AI Settings > Decision. Select a text-generation model for
  conversation profiles.
operator_action: |
  Jev uses /systemone, not /chat/completions. Keep decision and conversation
  model configuration independent.
---
