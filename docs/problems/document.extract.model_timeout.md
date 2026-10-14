---
code: document.extract.model_timeout
status: 504
title: Extraction timed out
summary: |
  The model did not answer within `DOCUMENT_MODEL_TIMEOUT`.
client_action: |
  Retry with a sharper, smaller photo.
operator_action: |
  Check `document_extract_duration_seconds`; local CPU models need the async mode (spec R2).
related_routes:
  - POST /api/documents/extract
---
