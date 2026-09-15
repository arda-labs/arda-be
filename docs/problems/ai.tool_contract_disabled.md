---
code: ai.tool_contract_disabled
status: 409
title: Tool disabled by contract
summary: |
  The tool's contract default is `enabled: false`, which is a hard floor: a
  runtime override can disable further but can never enable beyond the
  contract — `effectiveEnabled = contractEnabled AND (overrideEnabled ?? true)`
  (ADR-003).
client_action: |
  Do not retry. The tool cannot be enabled from the admin UI; it must ship with
  `enabled: true` in `contracts/ai-internal/*.json` and a new deployment.
operator_action: |
  Trace `request_id`; if the tool should become available, coordinate the
  contract change with the owning domain team and regenerate the catalog
  (`go run ./tools/catalog-gen`).
---

Emitted by `PATCH /api/ai/tools/{methodName}` with `{"enabled": true}` when the
contract hard-disables the tool.
