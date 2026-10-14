---
code: document.extract.image_too_large
status: 413
title: Image too large
summary: |
  An image exceeds 10 MiB or 40 megapixels.
client_action: |
  Downscale (≈2000 px long side) and retry.
operator_action: |
  Check `DOCUMENT_MAX_IMAGE_BYTES` if users hit it with normal photos.
related_routes:
  - POST /api/documents/extract
---
