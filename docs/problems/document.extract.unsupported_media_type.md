---
code: document.extract.unsupported_media_type
status: 415
title: Unsupported image format
summary: |
  Only JPEG and PNG are accepted (by content, not extension).
client_action: |
  Re-encode as JPEG and retry.
operator_action: |
  None.
related_routes:
  - POST /api/documents/extract
---
