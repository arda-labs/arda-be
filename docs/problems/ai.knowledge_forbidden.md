---
code: ai.knowledge_forbidden
status: 403
title: Knowledge access forbidden
summary: |
  The actor does not hold `ai.knowledge.read`, which gates retrieval of
  approved knowledge. `/api/rag/query` and `/api/rag/feedback` enforce the
  same permission as the `knowledge.search` tool, so a valid
  `ai.assistant.use` session alone is not enough.
client_action: |
  Do not retry unchanged. Show access denied and guide the user to request
  `ai.knowledge.read` from an IAM administrator.
operator_action: |
  Trace `request_id` and inspect the actor's `X-Permissions` and
  `X-Global-Permissions` for `ai.knowledge.read`. Grant it to the role that
  owns knowledge retrieval (the auth-gateway route `rag-query` also requires
  it), or use a superadmin/global-admin session.
---

Emitted by `POST /api/rag/query` and `POST /api/rag/feedback` when the
gateway-verified actor lacks `ai.knowledge.read`. The auth-gateway route
`rag-query`/`rag-feedback` requires the same permission; direct superadmin and
global-admin sessions bypass the check.
