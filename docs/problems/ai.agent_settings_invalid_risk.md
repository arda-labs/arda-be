---
code: ai.agent_settings_invalid_risk
status: 400
title: Invalid act-mode risk ceiling
summary: |
  The requested maximum risk for act mode is not allowed. Only `low` and `medium`
  are accepted; high-risk actions always require approval.
client_action: |
  Do not retry unchanged. Send `act_mode_max_risk` as `low` or `medium`.
operator_action: |
  No action needed unless the caller repeatedly sends an invalid value; verify the
  client uses the allowed enum.
related_routes:
  - PUT /api/ai/settings/agent
---
