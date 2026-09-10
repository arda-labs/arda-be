---
code: ai.approval_expiry_invalid
status: 400
title: Approval expiry invalid
summary: |
  `expiresInSeconds` is outside the accepted range of 60 to 3600 seconds.
client_action: |
  Send `expiresInSeconds` between 60 and 3600, or omit it to use the default
  of 900 seconds (15 minutes), and resend the proposal.
operator_action: |
  Trace `request_id` and check whether the client computed the expiry in the
  wrong unit (e.g. milliseconds) or reused a stale TTL configuration.
---

Emitted by `POST /api/ai/approvals/propose`. `0` is treated as "not supplied"
and maps to the 15-minute default; only explicit out-of-range values are
rejected.
