---
code: rag.invalid_query
status: 400
title: Invalid query
summary: |
  The `query` field of the knowledge search is blank or longer than 2000
  characters after trimming.
client_action: |
  Send a `query` string of 1–2000 characters and retry the search.
operator_action: |
  Trace `request_id` and check the client's search input handling — an empty
  box submitted verbatim, or unbounded paste content, triggers this code.
---

Emitted by `POST /api/rag/query` after JSON decoding succeeds; a body that is
not valid JSON returns [rag.invalid_json](rag.invalid_json.md) instead.
