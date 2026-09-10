---
code: ai.assistant_forbidden
status: 403
title: Assistant forbidden
summary: |
  The authenticated actor does not have the `ai.assistant.use` permission
  required for AI assistant endpoints.
client_action: |
  Do not retry unchanged. Show access denied and guide the user to request the
  `ai.assistant.use` permission.
operator_action: |
  Trace `request_id` and check the actor's `X-Permissions` list for
  `ai.assistant.use` (or `superadmin`), plus the auth-gateway policy route
  matched for the request.
---

Emitted before any payload parsing by the run handler (`POST /api/ai/agent`),
the conversation handlers (`GET`/`DELETE /api/ai/conversations*`), and the
feedback handler (`POST /api/ai/feedback`).
