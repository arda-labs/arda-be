---
code: ai.user_message_required
status: 400
title: User message required
summary: |
  The run body decoded fine but contains no usable user turn: no message has
  role `user` with non-blank content, and there is no HITL resume to replay.
client_action: |
  Include at least one message with `role: "user"` and non-empty `content`
  (or a `resume` array for continuing an interrupted run) and send the request
  again.
operator_action: |
  Trace `request_id` and inspect the client's message assembly — empty or
  whitespace-only user content, or a messages array of only system/assistant
  turns, triggers this code.
---

Emitted by `POST /api/ai/agent` after run identifier checks and resume
delegation, so it fires for fresh runs only — resume flows skip this check.
