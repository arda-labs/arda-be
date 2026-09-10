---
code: ai.invalid_feedback_input
status: 400
title: Invalid feedback input
summary: |
  The feedback body is not valid JSON, contains unknown fields, exceeds 8 KiB,
  or its `run_id` is blank or longer than 64 characters.
client_action: |
  Do not retry unchanged. Send a JSON object with `run_id` (1–64 chars),
  boolean `helpful`, and optional `comment`.
operator_action: |
  Trace `request_id` and check the client's body shape against the OpenAPI
  schema for `POST /api/ai/feedback`, including unknown fields (the decoder
  rejects them) and run id length.
---

The same code also maps rag-service's 422 reply when relaying feedback, so a
valid-looking body can still fail validation upstream in rag-service.
A valid run id for an unknown run returns
[ai.feedback_run_not_found](ai.feedback_run_not_found.md) instead.
