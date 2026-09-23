---
code: ai.agent_settings_save_failed
status: 500
title: Could not save agent settings
summary: |
  The AI service could not persist the tenant's act-mode (Ask/Act) configuration.
client_action: |
  Retry once. The previous setting is unchanged; if it persists, report the
  `request_id` to the operations team.
operator_action: |
  Correlate `request_id` and `trace_id` in the ai-service logs. Check the
  `ai_agent_settings` table and the database connection.
related_routes:
  - PUT /api/ai/settings/agent
---
