---
code: ai.unsupported_reasoning_budget
status: 400
title: Unsupported reasoning budget
summary: |
  The requested thinking-token budget is outside the accepted range.
client_action: |
  Send `0` (use the reasoning effort instead) or a value between 1024 and
  64000 and submit the profile again. Do not retry the unchanged request.
operator_action: |
  Trace `request_id`. The bounds are enforced by `model.ValidReasoningBudget`
  and by the `ai_model_profiles_reasoning_budget_check` constraint.
---

Emitted by `POST /api/ai/settings/profiles` and
`PUT /api/ai/settings/profiles/{id}` when `reasoningBudgetTokens` is invalid.
