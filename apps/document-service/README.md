# document-service

Stateless OCR for identity documents. v1 reads both sides of a Vietnamese
CCCD / Căn cước card through an OpenAI-compatible vision model and returns
normalized, cross-checked fields. It stores nothing and never logs card data.

Design: `arda-labs/docs/superpowers/specs/2026-10-14-document-ocr-cccd-design.md`.

Environment:

```
DOCUMENT_MODEL_PRESET=opencode-go | openai-compatible
DOCUMENT_MODEL_BASE_URL=https://opencode.ai/zen/go/v1
DOCUMENT_MODEL_NAME=qwen3.8-flash
DOCUMENT_MODEL_API_KEY=...            # without it a cloud model is disabled
DOCUMENT_MODEL_TIMEOUT=25s            # whole request incl. one retry; gateway allows 30s
DOCUMENT_REQUIRE_LOCAL=false          # production: true (refuses non-local models)
DOCUMENT_LOCAL_HOSTS=*.svc.cluster.local,localhost,127.0.0.1,192.168.10.0/24
```

`DOCUMENT_LOCAL_HOSTS` must list only hosts that run inference themselves; a LAN proxy that forwards to a cloud model must not be listed, or `DOCUMENT_REQUIRE_LOCAL` will accept it as local.

Dev uses a cloud model with fictional or sample cards only.

HTTP endpoints:

```
GET  /health/live
GET  /health/ready
GET  /metrics
GET  /api/documents/capabilities
POST /api/documents/extract   multipart: doc_type=cccd, front, back (optional); JPEG/PNG ≤ 10 MiB each
```

Accuracy: `tools/document-eval` (manual, not in CI).
