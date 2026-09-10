---
code: ai.idempotency_key_required
status: 400
title: Idempotency key required
summary: |
  The approval proposal carried no idempotency key, or the key exceeded 255
  characters.
client_action: |
  Send a non-empty `Idempotency-Key` header, or an `idempotencyKey` field in
  the proposal body, of at most 255 characters, and resend. The body field
  wins when both are present.
operator_action: |
  Trace `request_id` and check whether the client dropped the
  `Idempotency-Key` header on retry or generated over-long keys.
---

Emitted by `POST /api/ai/approvals/propose`. The key powers safe replays: the
same key returns the stored proposal (HTTP 200) instead of creating a
duplicate, and a mismatched payload with a used key yields
[ai.idempotency_conflict](ai.idempotency_conflict.md).
