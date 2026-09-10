---
code: ai.approval_not_found
status: 404
title: Approval not found
summary: |
  No approval exists for this tenant under the given id, or the approvals URL
  is not a valid sub-path (`{id}/decision` or `{id}/execution`).
client_action: |
  Do not retry unchanged. Verify the approval id against the approvals list
  and refresh stale views; approvals expire and are tenant-scoped.
operator_action: |
  Trace `request_id`, confirm the approval exists in the ai-service database
  for the tenant, and check the URL shape and tenant scoping of the lookup.
---

Emitted by the decision handler (`POST /api/ai/approvals/{id}/decision`), the
execution handler (`POST /api/ai/approvals/{id}/execution`), and AG-UI resume
entries whose `interruptId` matches no approved execution. An expired or
already-decided approval returns `ai.approval_expired` or
[ai.approval_not_pending](ai.approval_not_pending.md) instead.
