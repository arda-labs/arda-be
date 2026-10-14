---
code: document.extract.model_unavailable
status: 503
title: Extraction unavailable
summary: |
  No model is configured, or the provider failed.
client_action: |
  Enter data manually; retry later.
operator_action: |
  Check `DOCUMENT_MODEL_*` config, the API key in `arda-app-secrets`, and provider status.
related_routes:
  - POST /api/documents/extract
---
