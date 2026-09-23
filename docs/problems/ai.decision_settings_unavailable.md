---
code: ai.decision_settings_unavailable
status: 503
title: Decision model settings unavailable
summary: |
  The AI service could not read or save this tenant's decision model settings.
client_action: |
  Retry once. Do not assume displayed defaults or unsaved changes are active.
operator_action: |
  Verify the ai_decision_settings migration, database connectivity and the
  service encryption secret. New nonempty keys must be encrypted at rest.
related_routes:
  - GET /api/ai/settings/decision
  - PUT /api/ai/settings/decision
  - POST /api/ai/settings/decision/test
---
