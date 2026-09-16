---
code: ai.approval_stale
status: 409
title: Approval is stale
summary: |
  The permission snapshot recorded when the AI proposal was created no longer
  matches the caller's current `X-Auth-Version`. The action may have been
  reviewed while the actor still held permissions that have since changed.
client_action: |
  Do not retry this approval. Create a new proposal from the current session so
  the approver reviews the action against the up-to-date permission state.
operator_action: |
  Compare `ai_approvals.permission_version` with the `X-Auth-Version` injected
  by the auth-gateway and check IAM for role/permission changes between
  proposal creation and resume. The execution is finished as `FAILED` with
  error code `ai.approval_stale` and the tool never runs.
---

Emitted by `POST /api/ai/approvals/{id}/execution` and the AG-UI resume entries
on `POST /api/ai/agent` when the stored `permission_version` differs from the
caller's live auth version. The service fails closed by design: a changed
permission snapshot invalidates the human review.
