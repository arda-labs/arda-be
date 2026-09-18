---
code: media.preview.too_large
status: 413
title: File too large to preview
summary: |
  The document exceeds the conversion limit (`preview_max_size_mb`, default
  25MB) and is not converted for inline preview.
client_action: |
  Do not retry unchanged. Show the file card and let the user download the
  original document.
operator_action: |
  Trace `request_id` and compare `media_files.size_bytes` with
  `preview_max_size_mb`; raise the limit deliberately only when the Gotenberg
  pod can absorb the conversion cost.
---

Returned by `GET /api/media/{public_id}/preview` before calling Gotenberg, so
large uploads never occupy LibreOffice time or storage for a derived PDF.
