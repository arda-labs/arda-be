---
code: ai.approval_forbidden
status: 403
title: Approval forbidden
summary: |
  The authenticated actor lacks `ai.assistant.use` or the route-specific
  approval permission (`ai.approval.propose` or `ai.approval.execute`).
client_action: |
  Do not retry unchanged. Show access denied and guide the user to request the
  missing approval permission.
operator_action: |
  Trace `request_id` and check the actor's `X-Permissions` for both
  `ai.assistant.use` and the permission implied by the route:
  `ai.approval.propose` on proposal creation, `ai.approval.execute` on
  decisions and executions. Compare with the matched `policy.yaml` route.
---

Emitted by every handler that goes through the approvals scope helper:
`POST /api/ai/approvals/propose`, `GET /api/ai/approvals`,
`POST /api/ai/approvals/{id}/decision`, and
`POST /api/ai/approvals/{id}/execution`.
