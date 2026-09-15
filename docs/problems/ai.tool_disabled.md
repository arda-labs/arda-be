---
code: ai.tool_disabled
status: 403
title: Tool disabled
summary: |
  A platform administrator disabled the tool at runtime
  (`ai_tool_settings.enabled = false`), so it is excluded from the
  model-visible catalog and cannot be proposed for approval while disabled.
client_action: |
  Do not retry unchanged. Pick another tool or ask a platform admin to re-enable
  it; `GET /api/ai/tools` reports the effective state of every tool.
operator_action: |
  Trace `request_id` and check `ai_tool_settings` for the method. The
  `arda.ai.audit.tool_governance_changed` event records who set or cleared the
  override (ADR-003).
---

Emitted by `POST /api/ai/approvals` when a FE-initiated proposal targets a
runtime-disabled tool.
