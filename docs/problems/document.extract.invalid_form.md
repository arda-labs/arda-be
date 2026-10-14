---
code: document.extract.invalid_form
status: 400
title: Invalid upload
summary: |
  The request is not a valid multipart upload.
client_action: |
  Send multipart/form-data with `doc_type`, `front` and optional `back`.
operator_action: |
  None unless widespread; check client version.
related_routes:
  - POST /api/documents/extract
---
