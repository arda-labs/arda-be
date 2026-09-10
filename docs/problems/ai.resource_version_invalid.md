---
code: ai.resource_version_invalid
status: 400
title: Resource version invalid
summary: |
  `resourceVersion` in the approval proposal exceeds 255 characters.
client_action: |
  Send a `resourceVersion` of at most 255 characters (or omit it) and resend
  the proposal.
operator_action: |
  Trace `request_id` and check how the client derives the version string for
  the guarded CRM resource — over-long values usually indicate a serialization
  bug rather than user input.
---

Emitted by `POST /api/ai/approvals/propose`. The value is stored on the
approval record and echoed back in the proposal response for optimistic
concurrency checks on the client side.
