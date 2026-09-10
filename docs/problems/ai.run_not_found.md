---
code: ai.run_not_found
status: 404
title: Run not found
summary: |
  The approval proposal references a run (`threadId`/`runId`) that does not
  exist for this tenant, so the proposal cannot be attached to it.
client_action: |
  Do not retry unchanged. Use the thread and run ids exactly as returned by
  the original agent run, and only propose while that run is alive.
operator_action: |
  Trace `request_id`, confirm the run exists in the ai-service database for
  the tenant, and check tenant scoping of the proposal lookup.
---

Emitted by `POST /api/ai/approvals/propose` when the approval store cannot
find the referenced run. A run that exists but is not awaiting a decision
returns `ai.run_not_awaiting_approval` (409) instead.
