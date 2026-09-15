---
code: ai.invalid_pagination
status: 400
title: Invalid pagination
summary: |
  The `limit` or `cursor` query parameter on `GET /api/ai/tools` is not an
  integer within the accepted range (`limit` 1-200, `cursor` >= 0).
client_action: |
  Do not retry unchanged. Fix `limit`/`cursor` and retry; omit both parameters
  to receive the full catalog array.
operator_action: |
  Trace `request_id`; a repeat offender is usually a frontend bug building the
  query string. Compare the values with the paging contract of
  `GET /api/ai/tools`.
---

Emitted by `GET /api/ai/tools` when explicit paging parameters are malformed or
out of range.
