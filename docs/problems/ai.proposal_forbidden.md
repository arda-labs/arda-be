---
code: ai.proposal_forbidden
status: 403
title: Proposal forbidden
summary: |
  The proposal body was valid, but the actor lacks `crm.customer.read`, the
  data permission backing the `crm.customer.export.prepare` proposal.
client_action: |
  Do not retry unchanged. Request the `crm.customer.read` permission, then
  propose the export again.
operator_action: |
  Trace `request_id` and check the actor's `X-Permissions` for
  `crm.customer.read` in addition to `ai.approval.propose`, and the
  auth-gateway policy route for the proposals endpoint.
---

Emitted only by `POST /api/ai/approvals/propose` after the proposal input and
arguments validated, mirroring that an approver must be able to read the
customer data the export will touch.
