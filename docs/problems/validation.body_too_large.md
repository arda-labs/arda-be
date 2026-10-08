---
code: validation.body_too_large
status: 413
title: Request body exceeds the endpoint limit
summary: |
  The request was rejected before it was read because its declared
  `Content-Length` exceeds the byte budget for that route.
client_action: |
  Do not retry unchanged. Split the payload, or use the endpoint that is
  designed for files of this size (uploads, RAG sources, BPMN definitions and
  posting sheets each have their own raised budget).
operator_action: |
  Trace `request_id`. The limit is set per route by
  `LimitBodyMiddlewareByPrefix`; raise the matching `BodyLimitOverride` only
  after confirming the receiving pod has the memory limit to absorb it, and
  never by widening the service default.
---

Emitted by the request-body limit middleware before the handler runs, so an
oversize body is refused rather than buffered. The same middleware keeps
`http.MaxBytesReader` underneath for chunked requests that declare no length,
which are cut off mid-read with the same code.
