---
code: ai.invalid_run_input
status: 400
title: Invalid run input
summary: |
  The AG-UI run body could not be decoded as JSON, or exceeded the 1 MiB
  request limit.
client_action: |
  Do not retry unchanged. Send a valid JSON run object (`threadId`, `runId`,
  `messages`, optional `tool`, `state`, `context`, `resume`) with a body under
  1 MiB.
operator_action: |
  Trace `request_id`, compare the payload against the OpenAPI schema for
  `POST /api/ai/agent`, and check for truncated bodies at proxies.
---

Emitted before any identifier validation: a syntactically valid body missing
`threadId`/`runId` returns `ai.run_identifiers_required` instead, and a
non-empty unsupported `protocolVersion` returns
`ai.protocol_version_unsupported`.
