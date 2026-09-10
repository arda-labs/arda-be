---
code: rag.invalid_json
status: 400
title: Invalid json
summary: |
  A RAG knowledge endpoint received a request body that is not valid JSON.
client_action: |
  Do not retry unchanged. Send a well-formed JSON body for the endpoint —
  `POST /api/rag/query` (`query`), `/api/rag/feedback` (`runId`, `helpful`,
  `comment`), source/version creation, preview-chunks, and review requests.
operator_action: |
  Trace `request_id`, identify which RAG route logged the failure, and check
  the client's serialization and any proxy that may truncate the body.
---

Shared by the RAG knowledge routes; field-level failures after decoding have
their own codes, such as [rag.invalid_query](rag.invalid_query.md),
`rag.invalid_scope`, and `rag.preview_failed`.
