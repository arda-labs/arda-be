---
code: ai.tool_not_found
status: 404
title: Tool not found
summary: |
  The tool name (or the exact requested version) is not registered, so the
  call cannot be resolved for this run.
client_action: |
  Do not retry unchanged. Verify the tool name and version against the tool
  catalog (`GET /api/ai/tools`); a version mismatch also reports the tool as
  unknown. In streaming runs the miss arrives as a `TOOL_CALL_RESULT` SSE
  event with `error: ai.tool_not_found`.
operator_action: |
  Trace `request_id` and compare the requested tool name/version with the
  registered catalog; check whether the tool was renamed, disabled, or the
  client pinned a stale version.
---

Emitted for an explicit `tool` object in the run body (`POST /api/ai/agent`)
and for model-initiated tool calls during the agent stream.
