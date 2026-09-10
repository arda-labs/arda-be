---
code: ai.tool_forbidden
status: 403
title: Tool forbidden
summary: |
  The tool registry denied the call: the tool definition requires permissions
  the actor does not hold, or the execution context lost its tenant/user scope.
client_action: |
  Do not retry unchanged. Show access denied and guide the user to request the
  tool's required permission. In streaming runs the denial also arrives as a
  `TOOL_CALL_RESULT` SSE event with `error: ai.tool_forbidden`.
operator_action: |
  Trace `request_id` and compare the tool definition's required permissions
  with the actor's `X-Permissions`. For approved HITL executions, check that
  the stored tool still resolves with kind `confirm` — otherwise the execution
  is marked `FAILED` and denied.
---

Emitted for explicit `tool` calls in the run body (`POST /api/ai/agent`),
model-initiated tool calls during the agent stream (also published as an
`arda.ai.audit.tool_denied` event), and approved executions under
`POST /api/ai/approvals/{id}/execution` and AG-UI resume.
