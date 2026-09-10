---
code: ai.agent_id_required
status: 400
title: Agent id required
summary: |
  The agent URL path did not include an agent id, so `DELETE /api/ai/agents/{id}`
  has nothing to delete.
client_action: |
  Do not retry unchanged. Send the agent id as the last path segment, e.g.
  `DELETE /api/ai/agents/<agentId>`.
operator_action: |
  Trace `request_id` and check the caller's path template — the handler
  requires at least four path segments with a non-empty id.
---

Emitted only by the agent deletion handler. A well-formed path with an unknown
id still returns 200 with `{"deleted": true}` (idempotent delete), so this code
signals a malformed URL, not a missing agent.
