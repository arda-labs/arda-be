---
code: ai.tool_update_failed
status: 500
title: Tool update failed
summary: |
  The enable/disable override could not be persisted in `ai_tool_settings`; the
  previous governance state is unchanged.
client_action: |
  Retry a limited number of times with backoff. If it persists, report the
  `request_id` to the operations team.
operator_action: |
  Correlate `request_id` in the ai-service logs, check database health for the
  AI database, and verify the goose migration `20260915100000_ai_tool_settings.sql`
  applied.
---

Emitted by `PATCH /api/ai/tools/{methodName}` when the override write or delete
fails.
