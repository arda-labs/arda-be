---
code: ai.invalid_approval_input
status: 400
title: Invalid approval input
summary: |
  The proposal body is not valid JSON, contains unknown fields, has trailing
  content, or exceeds the 64 KiB body limit.
client_action: |
  Do not retry unchanged. Send a JSON object with exactly the documented
  fields (`threadId`, `runId`, `tool`, and the optional `resourceVersion`,
  `idempotencyKey`, `expiresInSeconds`).
operator_action: |
  Trace `request_id`, compare the payload against the OpenAPI schema for
  `POST /api/ai/approvals/propose`, and check for unknown fields, duplicated
  JSON values, or an oversized body.
---

The decoder runs with `DisallowUnknownFields`, so any extra property is
rejected. Field-level problems after decoding — missing thread/run ids, a tool
outside the allowlist — return their own codes
(`ai.run_identifiers_required`, `ai.proposal_not_allowlisted`).
