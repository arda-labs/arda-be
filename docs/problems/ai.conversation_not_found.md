---
code: ai.conversation_not_found
status: 404
title: Conversation not found
summary: |
  The conversation thread does not exist for this actor and tenant, or the URL
  is not a valid conversation path (`{threadId}` for delete,
  `{threadId}/messages` for history).
client_action: |
  Do not retry unchanged. Verify the thread id against
  `GET /api/ai/conversations` and refresh stale views; threads are scoped to
  the calling user, so another user's thread id also 404s.
operator_action: |
  Trace `request_id`, confirm the thread exists in the ai-service database,
  and check the tenant and actor-user scoping of the lookup.
---

Emitted by the messages and delete handlers for malformed paths (empty or
over-long thread ids, wrong suffix) and when the store reports the thread
missing for the tenant/user pair.
