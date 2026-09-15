---
code: ai.invalid_tool_update
status: 400
title: Invalid tool update
summary: |
  The `PATCH /api/ai/tools/{methodName}` body must contain exactly one of
  `{"enabled": true|false}` (upsert the runtime override) or
  `{"clearOverride": true}` (delete it and return to the contract default).
client_action: |
  Do not retry unchanged. Send exactly one of the two shapes with a boolean
  value; `clearOverride` accepts only `true`.
operator_action: |
  Trace `request_id` and inspect the request body. The endpoint rejects unknown
  fields and non-boolean values so the override state stays unambiguous
  (ADR-003).
---

Emitted by `PATCH /api/ai/tools/{methodName}` for malformed, ambiguous, or
unknown-field request bodies.
