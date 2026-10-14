---
code: document.extract.unsupported_doc_type
status: 400
title: Unsupported document type
summary: |
  `doc_type` is missing or not supported (only `cccd`).
client_action: |
  Send `doc_type=cccd`; read `GET /api/documents/capabilities`.
operator_action: |
  None.
related_routes:
  - POST /api/documents/extract
---
