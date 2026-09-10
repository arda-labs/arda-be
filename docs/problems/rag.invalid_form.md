---
code: rag.invalid_form
status: 400
title: Invalid form
summary: |
  The multipart form on `POST /api/rag/sources/parse-preview` could not be
  parsed, typically because the body exceeds the 32 MiB form limit or the
  multipart encoding is malformed.
client_action: |
  Send a `multipart/form-data` body under 32 MiB containing a `file` part
  (plus optional `chunk_size`/`chunk_overlap` fields) and retry.
operator_action: |
  Trace `request_id` and check the client's multipart encoder, the declared
  `Content-Type`, and whether a proxy stripped the body of large uploads.
---

Emitted by the parse-preview endpoint before the `file` part is read; a
parsed form without a `file` part returns `rag.missing_file` instead.
