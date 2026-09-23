---
code: ai.agent_settings_fetch_failed
status: 500
title: Agent settings unavailable
summary: |
  The AI service could not read the tenant's act-mode (Ask/Act) configuration.
client_action: |
  Retry once. If it persists, report the `request_id` to the operations team — the
  Ask/Act settings screen cannot be trusted to show the current state.
operator_action: |
  Correlate `request_id` and `trace_id` in the ai-service logs. Confirm the
  `ai_agent_settings` migration ran and that the AI database is reachable.
related_routes:
  - GET /api/ai/settings/agent
---
