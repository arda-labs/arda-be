---
code: ai.result_not_found
status: 404
title: Result not found
summary: |
  The execute() result id is unknown, has expired, or belongs to another
  thread or user. The result store is namespaced by tenant, user, and thread,
  so these cases are deliberately indistinguishable.
client_action: |
  Do not retry unchanged. Verify the `result_id` and `thread_id` pair, and
  treat the widget data as unavailable; ask the assistant to run the query
  again to get a fresh result id.
operator_action: |
  No action needed in normal operation: stored results are short-lived. If it
  happens for fresh ids, trace `request_id` and check that the sandbox result
  store is enabled and that the thread id matches the one used by execute().
related_routes:
  - GET /api/ai/results/{resultId}
---

Emitted by `GET /api/ai/results/{resultId}?thread_id=...` when the sandbox
result store has no entry for the caller's tenant/user/thread namespace, or
when no result reader is configured.
