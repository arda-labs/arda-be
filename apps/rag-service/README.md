# rag-service

Python RAG microservice — ingestion, hybrid retrieval, and reranking.

## Architecture

See [docs/ai/rag-service-design.md](../../docs/ai/rag-service-design.md) for the full design spec,
data model, and API contract.

## Run locally

```bash
cd apps/rag-service
python -m venv .venv
.venv/Scripts/pip install -e .
```

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `RAG_DB_DSN` | Yes | — | Postgres DSN (e.g. `postgres://user:pass@host:5432/rag`) |
| `RAG_AUTH_SECRET` | Yes | — | Shared HMAC secret for identity token verification |
| `RAG_EMBEDDING__MODEL` | No | `@cf/qwen/qwen3-embedding-0.6b` | Embedding model name |
| `RAG_EMBEDDING__BASE_URL` | No | — | OpenAI-compatible /embeddings endpoint |
| `RAG_EMBEDDING__API_KEY_ENV` | No | `CF_WORKERS_AI_API_TOKEN` | Env var holding the embedding API key |
| `RAG_RERANKER__PROVIDER` | No | `none` | Reranker: `none` or `anthropic` |
| `RAG_RERANKER__API_KEY_ENV` | No | `ANTHROPIC_API_KEY` | Env var holding the Anthropic key |

All settings are prefixed with `RAG_`. Nested sections use `__` delimiter, e.g.
`RAG_EMBEDDING__DIMENSIONS=1024`.

### Start the API server

```bash
RAG_DB_DSN="postgres://..." RAG_AUTH_SECRET="..." uvicorn app.main:app --host 0.0.0.0 --port 8099
```

### Start the ingestion worker

```bash
RAG_DB_DSN="postgres://..." RAG_AUTH_SECRET="..." python -m app.worker
```

## Project layout

```
app/
  config.py       — pydantic-settings model
  main.py         — FastAPI app factory + module-level app
  api/            — HTTP routes
  db/             — engine, session, migrations
  domain/         — domain models & logic
  worker.py       — CLI: ingestion worker loop
migrations/       — SQL migration files (Goose-compatible)
tests/
  unit/           — isolated unit tests
  integration/    — integration tests (require DB)