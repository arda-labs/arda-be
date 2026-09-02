# Rag-service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the rag-service Python microservice for RAG-based knowledge retrieval, replacing the ad-hoc SQL hybrid search in ai-service.

**Architecture:** FastAPI service with separate API/worker Deployments, PostgreSQL pgvector for storage, LlamaIndex for chunking/embedding, custom hybrid retriever (FTS + vector + RRF), Anthropic Haiku reranker, Postgres-backed durable queue for ingestion.

**Tech Stack:** Python 3.12, FastAPI, SQLAlchemy 2.0 + psycopg v3, LlamaIndex 0.12.4, Anthropic SDK, pgvector, SQL migrations

**Spec:** `docs/ai/rag-service-design.md`

## Global Constraints

- Python 3.12+ only, no Java/Node.js sidecars
- `llama-index-core==0.12.4`, `llama-index-vector-stores-postgres==0.6.3`, `llama-index-embeddings-openai==0.5.8` — exact pins, no `^`
- `psycopg[binary]>=3.2` — psycopg v3, not v2
- `httpx` is NOT a dependency — LlamaIndex embedder handles HTTP
- All new tables in `public` schema with `ai_` prefix
- `vector(1024)` + embedding model is immutable Phase 1 contract — dimension mismatch → job FAILED
- Migration naming: `^\d{14}_[a-z0-9][a-z0-9_-]*\.sql$` with `-- +goose Up` / `-- +goose Down` markers (per `scripts/check-migrations.mjs`)
- Migration runs at startup with `pg_advisory_lock` coordination (2 API replicas). Migrations are plain `.sql` files in `migrations/`, applied by a Python runner.
- Security: HMAC-SHA256 signed identity token `v1.{base64(claims)}.{base64(hmac)}`, TTL ≤ 5 min, verify signature + audience + expiry
- Security: `tenant_id`, `user_id`, `permissions` ONLY from SecurityContext (server-derived), never from request body
- Request body for query: only `{query, top_k}`, never identity fields
- `top_k` bounds: `1 ≤ top_k ≤ 10`, server-side enforced
- Python HMAC implementation must match Go `identity.go` contract exactly (same encoding, same claims structure)
- The existing `ai_knowledge_sources` / `ai_knowledge_chunks` tables (from Go migration) are preserved/renamed during migration — the Go ai-service must be scaled to 0 first (see P4.3)
- Cross-repo: `arda-infra` deployment manifests are NOT part of this plan (separate infra workstream, P4.4)

## Conventions

- Working directory for all commands: `arda-be/apps/rag-service/`
- Test runner: `python -m pytest` (pytest added as dev dependency)
- Vietnamese source content in tests is UTF-8; do not use `Set-Content -Encoding UTF8` on PowerShell — write files via editor tools only
- Every service module lives under `app/` with packages: `app/api`, `app/domain`, `app/service`, `app/rag`, `app/db`
- DB access via SQLAlchemy 2.0 Core (not ORM models for the RAG tables — raw SQL keeps framework-independence, per spec §3); psycopg v3 sync driver, `create_async_engine` NOT used in Phase 1 (worker is sync, API uses sync-in-threadpool or SQLAlchemy `run_in_executor`; keep it simple with sync engine + FastAPI threadpool)

**Determination of sync vs async:** The spec's LlamaIndex + psycopg stack is sync-first. Phase 1 uses sync SQLAlchemy engines. FastAPI routes are `def` (not `async def`) so they run in the threadpool; uvicorn with ≥2 workers handles concurrency. The retriever calls the embedding API synchronously via LlamaIndex.

## File Structure Map

```
apps/rag-service/
├── pyproject.toml               # deps, exact LlamaIndex pins
├── Dockerfile                   # multi-stage, uvicorn app.main:app OR python -m app.worker (ENTRYPOINT arg)
├── .dockerignore
├── README.md                    # run, env vars, architecture
├── migrations/
│   ├── 20260903090000_rag_foundation.sql   # T1
│   └── 20260903090001_rag_eval.sql         # T1
├── app/
│   ├── __init__.py
│   ├── main.py                  # FastAPI app factory, lifespan (migrate + warmup)
│   ├── config.py                # pydantic-settings Settings
│   ├── domain/
│   │   ├── __init__.py
│   │   ├── errors.py            # typed exceptions → HTTP mapping
│   │   ├── models.py            # pydantic request/response schemas
│   │   └── security.py          # identity token verify + SecurityContext
│   ├── db/
│   │   ├── __init__.py
│   │   ├── engine.py            # sync engine factory, DSN from env
│   │   ├── migrate.py           # advisory-lock SQL runner
│   │   ├── schema.py            # SQLAlchemy Table definitions (sources, versions, chunks, jobs, runs, feedback)
│   │   └── queue.py             # SKIP LOCKED claim, requeue, heartbeat, complete
│   ├── rag/
│   │   ├── __init__.py
│   │   ├── chunker.py           # heading-based chunker (LlamaIndex or stdlib)
│   │   ├── embedder.py          # LlamaIndex OpenAIEmbedding wrapper + dimension guard
│   │   ├── retriever.py         # FTS + pgvector SQL, RRF fusion
│   │   ├── reranker.py          # Protocol + anthropic impl + none impl
│   │   └── pipeline.py          # query pipeline orchestrator
│   ├── service/
│   │   ├── __init__.py
│   │   ├── ingestion.py         # create version, review, publish, job lifecycle
│   │   ├── query_service.py     # /query handler orchestration + ai_rag_runs insert
│   │   ├── sources.py           # CRUD sources/versions
│   │   └── feedback.py          # /feedback
│   ├── api/
│   │   ├── __init__.py
│   │   ├── deps.py              # dependency: SecurityContext from request
│   │   ├── query.py             # POST /api/rag/query
│   │   ├── sources.py           # sources + versions routes
│   │   ├── jobs.py              # GET /api/rag/jobs/{id}
│   │   ├── feedback.py          # POST /api/rag/feedback
│   │   └── health.py            # /health/live, /health/ready
│   └── worker.py                # `python -m app.worker`: poll loop, heartbeat
├── tests/
│   ├── conftest.py              # postgres fixture (docker or TEST_DATABASE_DSN), temp engine
│   ├── unit/                    # pure logic (chunker, security verify, rrf math, reranker protocol)
│   └── integration/             # db round-trips (queue, migration, retriever with seeded rows)
└── scripts/
    └── selfcheck.py             # local invariant check mirroring arda-be CI for this app
```

---

## PHASE P1 — Scaffold + schema + migrations + security middleware

### DECISION POINT — legacy Go `ai_knowledge_sources` / `ai_knowledge_chunks`

Production Postgres already has `public.ai_knowledge_sources` and
`public.ai_knowledge_chunks` created by the **ai-service Go** migrations
(UUID PK via `uuidv7()`, `source_key` UNIQUE, `chunks.source_id`, no
versioning, no `chunk_id`). The rag spec (§3) reuses the same table names
with a **different shape** (BIGSERIAL, `source_version_id`, `chunk_id`).
The sqlrunner applies `CREATE TABLE IF NOT EXISTS` — on the live DB that
silently keeps the Go shape and rag queries would break.

**Resolution (Phase 1):** apply a single explicit `ALTER` migration in
`20260903090000_rag_foundation.sql` that **renames the Go tables** to
`ai_knowledge_sources_v1` / `ai_knowledge_chunks_v1` first, then creates the
fresh rag-shape tables. Go code that reads those tables by name (the
ai-service `SQLSearcher`) will fail-fast against the DB until it is deleted
in P4 — acceptable mid-transition; run order is P1.2 migration → P4 rewrite
→ P4.3 Go cleanup.

**Pre-flight (before applying on prod):** scale ai-service to 0, `pg_dump`
the two tables. Confirmed with the user on 2026-09-03.

## Task P1.1: Service scaffold — pyproject, config, main, health endpoints

**Files:**
- Create: `arda-be/apps/rag-service/pyproject.toml`
- Create: `arda-be/apps/rag-service/README.md`
- Create: `arda-be/apps/rag-service/Dockerfile`
- Create: `arda-be/apps/rag-service/.dockerignore`
- Create: `arda-be/apps/rag-service/app/__init__.py`, `app/main.py`, `app/config.py`, `app/domain/__init__.py`, `app/db/__init__.py`, `app/api/__init__.py`, `app/api/health.py`
- Create: `arda-be/apps/rag-service/tests/unit/test_health.py`

**Interfaces:**
- Consumes: nothing (standalone)
- Produces:
  - `app.config.Settings` — pydantic-settings model with `db_dsn`, `service_name="rag-service"`, `auth_secret`, `trusted_sources: list[str]`, `embedding` section, `reranker` section, `retrieval` section, `worker` section (exact fields in P1.1 step 3)
  - `app.main:create_app(settings) -> FastAPI` and module-level `app = create_app(Settings())`
  - Routes: `GET /health/live`, `GET /health/ready` — no auth (probes)

- [ ] **Step 1: Write the failing test**

`tests/unit/test_health.py`:

```python
from fastapi.testclient import TestClient

from app.config import Settings
from app.main import create_app


def test_health_live_returns_ok():
    settings = Settings(db_dsn="sqlite:///:memory:")  # placeholder; engine not wired yet
    client = TestClient(create_app(settings))
    r = client.get("/health/live")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}
```

(Test uses a Settings instance with a dummy DSN; the app factory must not connect to the DB on import — DB connection happens in lifespan only when `migrate_on_startup=True`. To keep the test hermetic, `create_app(settings, migrate_on_startup=False)`.)

- [ ] **Step 2: Run test to verify it fails**

```bash
cd apps/rag-service
python -m pytest tests/unit/test_health.py -v
```
Expected: FAIL — `ModuleNotFoundError: app.main` (or `create_app` missing).

- [ ] **Step 3: Write pyproject.toml**

```toml
[project]
name = "rag-service"
version = "0.1.0"
description = "RAG knowledge service — ingestion, hybrid retrieval, reranking"
requires-python = ">=3.12"
dependencies = [
  "fastapi>=0.115",
  "uvicorn[standard]>=0.32",
  "sqlalchemy>=2.0",
  "psycopg[binary]>=3.2",
  "pydantic>=2.9",
  "pydantic-settings>=2.7",
  "llama-index-core==0.12.4",
  "llama-index-vector-stores-postgres==0.6.3",
  "llama-index-embeddings-openai==0.5.8",
  "anthropic>=0.49",
]

[dependency-groups]
dev = ["pytest>=8", "httpx>=0.27"]   # httpx ONLY for tests, never a runtime dep

[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.build_meta"

[tool.setuptools.packages.find]
include = ["app*"]
```

Note: no `httpx` in `dependencies` (spec §13); the test client needs it, so it lives in the dev group only.

- [ ] **Step 4: Write config.py, main.py, health.py**

`app/config.py`:

```python
from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class EmbeddingSettings(BaseSettings):
    """Immutable Phase-1 contract: model + dimensions must match stored vectors."""
    provider: str = "openai-compatible"
    model: str = "@cf/qwen/qwen3-embedding-0.6b"
    dimensions: int = 1024
    api_key_env: str = "CF_WORKERS_AI_API_TOKEN"
    base_url: str = ""   # OpenAI-compatible /embeddings endpoint
    batch_size: int = 16
    concurrency: int = 4


class RerankerSettings(BaseSettings):
    provider: str = "none"            # none | anthropic
    model: str = "claude-haiku-4-5-20251001"
    api_key_env: str = "ANTHROPIC_API_KEY"
    timeout_ms: int = 1500
    candidates: int = 8
    top_n: int = 3


class RetrievalSettings(BaseSettings):
    vector_top_k: int = 8
    fts_top_k: int = 8
    rrf_k: int = 60
    rerank_candidates: int = 8
    final_top_k: int = 3


class WorkerSettings(BaseSettings):
    lease_duration_sec: int = 300     # 5m
    heartbeat_interval_sec: int = 60
    poll_interval_sec: int = 5
    max_attempts: int = 3


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="RAG_", env_nested_delimiter="__")

    service_name: str = "rag-service"
    db_dsn: str = ""                   # required in production
    auth_secret: str = ""              # shared ARDA_SERVICE_AUTH_SECRET
    trusted_sources: list[str] = ["ai-service", "auth-gateway"]
    migrate_on_startup: bool = True
    embedding: EmbeddingSettings = Field(default_factory=EmbeddingSettings)
    reranker: RerankerSettings = Field(default_factory=RerankerSettings)
    retrieval: RetrievalSettings = Field(default_factory=RetrievalSettings)
    worker: WorkerSettings = Field(default_factory=WorkerSettings)
```

Note env naming: `RAG_DB_DSN`, `RAG_AUTH_SECRET`, `RAG_EMBEDDING__MODEL`, … Document in README.

`app/main.py`:

```python
from fastapi import FastAPI

from app.api.health import router as health_router
from app.config import Settings


def create_app(settings: Settings, migrate_on_startup: bool | None = None) -> FastAPI:
    app = FastAPI(title="rag-service", version="0.1.0", docs_url=None, redoc_url=None)
    app.state.settings = settings
    app.include_router(health_router)

    do_migrate = settings.migrate_on_startup if migrate_on_startup is None else migrate_on_startup
    if do_migrate:
        from app.db.engine import get_engine
        from app.db.migrate import run_migrations

        @app.on_event("startup")
        def _migrate() -> None:          # noqa: ANN202
            run_migrations(get_engine(settings))

    return app


app = create_app(Settings())
```

Note: `@app.on_event("startup")` is deprecated in newer FastAPI — if the installed FastAPI warns, switch to lifespan; keep it dependency-light for now. Use lifespan as of FastAPI ≥0.115 (the plan below standardizes on lifespan).

`app/api/health.py`:

```python
from fastapi import APIRouter

router = APIRouter()


@router.get("/health/live")
def live() -> dict[str, str]:
    return {"status": "ok"}


@router.get("/health/ready")
def ready() -> dict[str, str]:
    return {"status": "ok"}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
python -m pytest tests/unit/test_health.py -v
```
Expected: PASS.

- [ ] **Step 6: Dockerfile + .dockerignore + README**

`Dockerfile` (one image, two commands — api default, worker via `--entrypoint`):

```dockerfile
FROM python:3.12-slim AS base
WORKDIR /srv
COPY pyproject.toml ./
COPY app ./app
COPY migrations ./migrations
RUN pip install --no-cache-dir .
EXPOSE 8099
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8099"]
```

`.dockerignore`:

```
__pycache__/
*.pyc
.pytest_cache/
tests/
.venv/
```

`README.md` — short: run instructions (env vars), architecture pointer to `docs/ai/rag-service-design.md`, `python -m app.worker` for the worker, port 8099.

- [ ] **Step 7: Commit**

```bash
cd arda-be
git add apps/rag-service/
git commit -m "feat(rag): scaffold rag-service Python microservice (config, health)"
```

## Task P1.2: SQL migration files + advisory-lock runner

**Files:**
- Create: `arda-be/apps/rag-service/migrations/20260903090000_rag_foundation.sql`
- Create: `arda-be/apps/rag-service/migrations/20260903090001_rag_eval.sql`
- Create: `arda-be/apps/rag-service/app/db/engine.py`
- Create: `arda-be/apps/rag-service/app/db/migrate.py`
- Test: `arda-be/apps/rag-service/tests/integration/test_migrate.py`

**Interfaces:**
- Consumes: `app.config.Settings`
- Produces:
  - `app.db.engine.get_engine(settings) -> sqlalchemy.Engine` (sync, psycopg v3)
  - `app.db.migrate.run_migrations(engine) -> None` (advisory-lock, idempotent per-file via schema_version table)
  - Tables per spec §3.1–3.7
- Global constraints honored: migration filenames `^\d{14}_…\.sql$` + goose markers (check-migrations.mjs scans the tree), no `DROP … CASCADE`, no synthetic tenant default.

- [ ] **Step 1: Write the migration SQL**

`migrations/20260903090000_rag_foundation.sql` — exact DDL from spec §3.1–3.6 (ai_knowledge_sources, ai_knowledge_source_versions, ai_knowledge_chunks, ai_ingestion_jobs, ai_rag_runs, ai_rag_feedback) with `-- +goose Up` / `-- +goose Down` markers. **Critical deviation to handle at the top:** the Go tables `public.ai_knowledge_sources` and `public.ai_knowledge_chunks` already exist (created by ai-service migration). The rag spec keeps the same names but a different shape (source_version_id, no source_key, BIGINT ids). Resolution: migrate in place with `ALTER`/backfill — full exact SQL is in the spec §3; below is the header + the rename/backfill portion that must run first:

```sql
-- +goose Up
-- Rag-service owns the knowledge tables from this migration forward.
-- The ai-service Go schema (UUID PK, source_key, chunk.source_id, no
-- source_version) predates versioning; it is migrated in place:
--   * ids stay BIGSERIAL (re-cast from UUID where ai-service wrote them)
--   * an initial version row is created per existing source (version = old .version)
--   * chunks are re-pointed at source_version_id and re-chunked on next job
-- ai-service must be scaled to 0 before this runs (schema is shared).

-- [full spec §3.1 DDL]  -- see spec for column-by-column; do not copy Go shape
CREATE TABLE IF NOT EXISTS public.ai_knowledge_sources ( ...spec 3.1... );
-- ... 3.2 versions, 3.3 chunks, 3.4 jobs, 3.5 runs, 3.6 feedback ...
```

**The executor must copy the DDL verbatim from spec §3.1–3.6**, including `active_version_id BIGINT REFERENCES ai_knowledge_source_versions(id)`, `chunk_id TEXT NOT NULL UNIQUE`, `embedding vector(1024)`, HNSW index `(m=16, ef_construction=64)`, partial index `idx_ingestion_jobs_claim ON ai_ingestion_jobs(status, created_at) WHERE status='pending'`, `next_retry_at TIMESTAMPTZ`.

If the Go tables already exist in the target DB, the migration must `ALTER TABLE` them to the new shape (rename columns) rather than `CREATE TABLE IF NOT EXISTS` silently keeping the old shape — verify with `\d public.ai_knowledge_sources` and reconcile. Where reconciliation is too invasive for a single migration (e.g., re-chunking legacy rows), leave legacy rows untouched and let P4 backfill index them as new versions.

`migrations/20260903090001_rag_eval.sql` — spec §3.7 `ai_rag_eval`.

- [ ] **Step 2: Write the failing test**

`tests/integration/test_migrate.py` (requires a running Postgres; `TEST_DATABASE_DSN` env or default `postgresql+psycopg://postgres:postgres@localhost:5432/postgres`):

```python
import os

import pytest
from sqlalchemy import create_engine, text

from app.config import Settings
from app.db.engine import get_engine
from app.db.migrate import run_migrations

DSN = os.environ.get("TEST_DATABASE_DSN", "postgresql+psycopg://postgres:postgres@localhost:5432/postgres")


def _clean(engine) -> None:
    with engine.begin() as conn:
        conn.execute(text("DROP TABLE IF EXISTS ai_rag_eval, ai_rag_feedback, ai_rag_runs, ai_ingestion_jobs, ai_knowledge_chunks, ai_knowledge_source_versions, ai_knowledge_sources CASCADE"))
        conn.execute(text("DROP TABLE IF EXISTS rag_schema_version"))


def test_migrations_apply_and_are_idempotent():
    engine = get_engine(Settings(db_dsn=DSN, migrate_on_startup=False))
    _clean(engine)
    try:
        run_migrations(engine)
        run_migrations(engine)  # second run must be a no-op
        with engine.connect() as conn:
            tables = {row[0] for row in conn.execute(text("SELECT tablename FROM pg_tables WHERE schemaname='public'"))}
            assert {"ai_knowledge_sources", "ai_knowledge_source_versions", "ai_knowledge_chunks", "ai_ingestion_jobs", "ai_rag_runs", "ai_rag_feedback", "ai_rag_eval"} <= tables
    finally:
        _clean(engine)
        engine.dispose()
```

- [ ] **Step 3: Run test to verify it fails**

```bash
python -m pytest tests/integration/test_migrate.py -v
```
Expected: FAIL (module not found).

- [ ] **Step 4: Implement engine.py + migrate.py**

`app/db/engine.py`:

```python
from sqlalchemy import Engine, create_engine

from app.config import Settings


def get_engine(settings: Settings) -> Engine:
    return create_engine(settings.db_dsn, pool_pre_ping=True)
```

`app/db/migrate.py` — advisory lock + ordered `.sql` split on `-- +goose Up` / `-- +goose Down` (executor implements `run_migrations(engine)` that:
1. acquires `pg_advisory_lock(<const key>)` via `SELECT pg_advisory_lock(727301001)` (any single 32-bit key constant; document it)
2. creates `rag_schema_version (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ DEFAULT now())`
3. for each `.sql` in `migrations/` sorted by name, if filename not in table: execute the Up section in one transaction, insert filename
4. releases lock via `pg_advisory_unlock`

```python
import logging
import pathlib
from sqlalchemy import Engine, text

logger = logging.getLogger(__name__)
_MIGRATIONS_DIR = pathlib.Path(__file__).resolve().parents[2] / "migrations"
_ADVISORY_LOCK_KEY = 727301001  # rag-service migration lock


def _load_sql_blocks(path: pathlib.Path) -> tuple[str | None, str | None]:
    src = path.read_text(encoding="utf-8")
    up, down = None, None
    in_block = None
    lines = src.splitlines()
    block = []
    for line in lines:
        stripped = line.strip()
        if stripped == "-- +goose Up":
            if up is None:
                in_block, block = "up", []
            continue
        if stripped == "-- +goose Down":
            if in_block == "up":
                up = "\n".join(block)
            in_block, block = "down", []
            continue
        if in_block:
            block.append(line)
    if in_block == "down":
        down = "\n".join(block)
    return up, down


def run_migrations(engine: Engine) -> None:
    """Apply pending migrations under a Postgres advisory lock.

    rag-api runs 2 replicas; the lock guarantees a single writer so two
    instances never apply the same migration concurrently (spec §3.8).
    """
    with engine.begin() as conn:
        conn.execute(text("SELECT pg_advisory_lock(:key)"), {"key": _ADVISORY_LOCK_KEY})
        try:
            conn.execute(text(
                "CREATE TABLE IF NOT EXISTS rag_schema_version ("
                " filename TEXT PRIMARY KEY,"
                " applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"
            ))
            applied = {row[0] for row in conn.execute(text("SELECT filename FROM rag_schema_version"))}
            for path in sorted(_MIGRATIONS_DIR.glob("*.sql")):
                if path.name in applied:
                    continue
                up, _down = _load_sql_blocks(path)
                if not up:
                    raise RuntimeError(f"{path.name}: empty Up block")
                conn.execute(text(up))
                conn.execute(
                    text("INSERT INTO rag_schema_version (filename) VALUES (:f)"),
                    {"f": path.name},
                )
                logger.info("applied migration %s", path.name)
        finally:
            conn.execute(text("SELECT pg_advisory_unlock(:key)"), {"key": _ADVISORY_LOCK_KEY})
```

- [ ] **Step 5: Run tests to verify they pass** (needs a local Postgres with pgvector enabled; `TEST_DATABASE_DSN`)

```bash
python -m pytest tests/integration/test_migrate.py -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): foundation schema migrations + advisory-lock startup runner"
```

## Task P1.3: Security middleware — identity token verify + SecurityContext

**Files:**
- Create: `arda-be/apps/rag-service/app/domain/security.py`
- Create: `arda-be/apps/rag-service/app/api/deps.py`
- Test: `arda-be/apps/rag-service/tests/unit/test_security.py`
- Modify: `arda-be/apps/rag-service/app/main.py` (include query/router middleware + fixtures)

**Interfaces:**
- Consumes: `Settings.auth_secret`, `Settings.trusted_sources`
- Produces:
  - `security.verify_service_token(token: str, secret: str, expected_audience: str, now: datetime | None = None) -> VerifiedClaims` where `VerifiedClaims` is a frozen dataclass `(source: str, audience: str)`
  - `security.SecurityContext` frozen dataclass: `tenant_id: str | None`, `user_id: str | None`, `permissions: tuple[str, ...]`, `source_service: str`, `auth_checked: bool`
  - `api.deps.security_context(request) -> SecurityContext` FastAPI dependency: reads `x-service-auth`, verifies, then builds context from `X-Tenant-Id`, `X-User-Id`, `X-Permissions` ONLY when source ∈ trusted; raises `HTTPException(401)` otherwise. **X-Permissions honored only when source == "auth-gateway"**.

- [ ] **Step 1: Write the failing test — a Go-issued token must verify**

`tests/unit/test_security.py`. The reference vector comes from the Go `identity` package; a Go token looks like:
`v1.<base64url(json claims)>.` `<base64url(hmac_sha256(secret, "v1."+payload))>` with claims `{"v":"v1","src":"ai-service","aud":"rag-service","iat":…,"exp":…,"nonce":"…"}`.

```python
import base64
import hashlib
import hmac
import json
import time

import pytest

from app.domain import security


def _go_style_token(secret: str, src: str, aud: str, iat: int, exp: int) -> str:
    claims = {
        "v": "v1",
        "src": src,
        "aud": aud,
        "iat": iat,
        "exp": exp,
        "nonce": base64.urlsafe_b64encode(b"0123456789abcdef").rstrip(b"=").decode(),
    }
    payload = base64.urlsafe_b64encode(json.dumps(claims, separators=(",", ":")).encode()).rstrip(b"=").decode()
    signing = hmac.new(secret.encode(), f"v1.{payload}".encode(), hashlib.sha256).digest()
    sig = base64.urlsafe_b64encode(signing).rstrip(b"=").decode()
    return f"v1.{payload}.{sig}"


SECRET = "a" * 32  # >= 32 chars per Go contract
NOW = int(time.time())


def test_verify_go_compatible_token():
    token = _go_style_token(SECRET, "ai-service", "rag-service", NOW - 10, NOW + 120)
    claims = security.verify_service_token(token, SECRET, "rag-service")
    assert claims.source == "ai-service"
    assert claims.audience == "rag-service"


def test_verify_rejects_wrong_audience():
    token = _go_style_token(SECRET, "ai-service", "other-service", NOW - 10, NOW + 120)
    with pytest.raises(security.AuthenticationError):
        security.verify_service_token(token, SECRET, "rag-service")


def test_verify_rejects_expired():
    token = _go_style_token(SECRET, "ai-service", "rag-service", NOW - 300, NOW - 60)
    with pytest.raises(security.AuthenticationError):
        security.verify_service_token(token, SECRET, "rag-service")


def test_verify_rejects_tampered_signature():
    token = _go_style_token(SECRET, "ai-service", "rag-service", NOW - 10, NOW + 120)
    parts = token.split(".")
    parts[1] = parts[1][:-2] + ("A" if parts[1][-1] != "A" else "B")
    with pytest.raises(security.AuthenticationError):
        security.verify_service_token(".".join(parts), SECRET, "rag-service")
```

- [ ] **Step 2: Run test to verify it fails** — expected `ModuleNotFoundError`.

- [ ] **Step 3: Implement security.py**

Must mirror Go `identity.go` **byte-for-byte**: claims keys `v/src/aud/iat/exp/nonce`; JSON compact separators `(",", ":")` for signing **but** Go marshals with `json.Marshal` (no spaces) — signing is over the exact payload bytes, so verification never re-marshals: it re-encodes what it decodes only for the HMAC input which is `v1.{encoded_payload}` as received. Key checks: 3 dot-parts, part[0]=="v1", decode base64url (Raw), version/src/aud/nonce non-empty, audience == expected, `iat>0`, `exp>iat`, `now ≥ iat-30s` (maxClockSkew), `now < exp`, constant-time HMAC compare. Raise `security.AuthenticationError` on any failure.

```python
import base64
import datetime as dt
import hashlib
import hmac
import json
from dataclasses import dataclass

_MAX_CLOCK_SKEW = dt.timedelta(seconds=30)


class AuthenticationError(Exception):
    """x-service-auth missing, malformed, or failed verification."""


@dataclass(frozen=True)
class VerifiedClaims:
    source: str
    audience: str


@dataclass(frozen=True)
class SecurityContext:
    tenant_id: str | None = None
    user_id: str | None = None
    permissions: tuple[str, ...] = ()
    source_service: str = ""
    auth_checked: bool = False


def _sign(secret: str, value: str) -> str:
    digest = hmac.new(secret.encode(), value.encode(), hashlib.sha256).digest()
    return base64.urlsafe_b64encode(digest).rstrip(b"=").decode()


def verify_service_token(
    token: str,
    secret: str,
    expected_audience: str,
    now: dt.datetime | None = None,
) -> VerifiedClaims:
    if len(secret.strip()) < 32:
        raise AuthenticationError("secret too short")
    parts = token.split(".")
    if len(parts) != 3 or parts[0] != "v1" or not parts[1] or not parts[2]:
        raise AuthenticationError("malformed token")
    expected = _sign(secret, f"v1.{parts[1]}")
    if not hmac.compare_digest(expected, parts[2]):
        raise AuthenticationError("bad signature")
    try:
        claims = json.loads(base64.urlsafe_b64decode(parts[1] + "=" * (-len(parts[1]) % 4)))
    except Exception:
        raise AuthenticationError("bad claims") from None
    if claims.get("v") != "v1" or not claims.get("src") or not claims.get("aud") or not claims.get("nonce"):
        raise AuthenticationError("bad claims")
    if claims["aud"] != expected_audience.strip():
        raise AuthenticationError("wrong audience")
    now = (now or dt.datetime.now(dt.timezone.utc)).timestamp()
    iat, exp = claims.get("iat") or 0, claims.get("exp") or 0
    if iat <= 0 or exp <= iat or now < iat - _MAX_CLOCK_SKEW.total_seconds() or not (now < exp):
        raise AuthenticationError("invalid lifetime")
    return VerifiedClaims(source=claims["src"], audience=claims["aud"])
```

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Write the failing middleware test**

`tests/integration/test_security_middleware.py`:

```python
import base64
import hashlib
import hmac
import json
import time

from fastapi import FastAPI, Depends
from fastapi.testclient import TestClient

from app.config import Settings
from app.domain import security
from app.main import create_app
from app.api.deps import security_context

SECRET = "b" * 32


def _token(src: str, aud: str, now: int) -> str:
    claims = {"v": "v1", "src": src, "aud": aud, "iat": now - 5, "exp": now + 120,
              "nonce": base64.urlsafe_b64encode(b"n" * 16).rstrip(b"=").decode()}
    payload = base64.urlsafe_b64encode(json.dumps(claims, separators=(",", ":")).encode()).rstrip(b"=").decode()
    sig = base64.urlsafe_b64encode(
        hmac.new(SECRET.encode(), f"v1.{payload}".encode(), hashlib.sha256).digest()
    ).rstrip(b"=").decode()
    return f"v1.{payload}.{sig}"


def _app() -> FastAPI:
    settings = Settings(auth_secret=SECRET, trusted_sources=["ai-service", "auth-gateway"], db_dsn="")
    application = create_app(settings, migrate_on_startup=False)

    @application.get("/_probe")
    def _probe(ctx: security.SecurityContext = Depends(security_context)) -> dict:
        return {"tenant": ctx.tenant_id, "user": ctx.user_id, "perms": list(ctx.permissions),
                "source": ctx.source_service}

    return application


def test_missing_token_is_401():
    client = TestClient(_app())
    r = client.get("/_probe")
    assert r.status_code == 401


def test_bad_token_is_401():
    client = TestClient(_app())
    r = client.get("/_probe", headers={"x-service-auth": "v1.abc.def"})
    assert r.status_code == 401


def test_unknown_source_is_401_even_with_headers():
    client = TestClient(_app())
    now = int(time.time())
    # token valid, but source not in trusted_sources
    client = TestClient(_app())
    r = client.get("/_probe",
                   headers={"x-service-auth": _token("evil-service", "rag-service", now),
                            "X-Tenant-Id": "tenant-a", "X-User-Id": "user-1"})
    assert r.status_code == 401


def test_trusted_source_gets_context_from_headers():
    client = TestClient(_app())
    now = int(time.time())
    r = client.get("/_probe",
                   headers={"x-service-auth": _token("auth-gateway", "rag-service", now),
                            "X-Tenant-Id": "tenant-a", "X-User-Id": "user-1",
                            "X-Permissions": "ai.knowledge.manage,ai.knowledge.read"})
    assert r.status_code == 200
    body = r.json()
    assert body["tenant"] == "tenant-a" and body["user"] == "user-1"
    assert body["perms"] == ["ai.knowledge.manage", "ai.knowledge.read"]
    assert body["source"] == "auth-gateway"


def test_non_gateway_source_ignores_x_permissions():
    client = TestClient(_app())
    now = int(time.time())
    r = client.get("/_probe",
                   headers={"x-service-auth": _token("ai-service", "rag-service", now),
                            "X-Tenant-Id": "tenant-a", "X-Permissions": "ai.knowledge.manage"})
    assert r.status_code == 200
    body = r.json()
    assert body["tenant"] == "tenant-a"          # tenant still propagates
    assert body["perms"] == []                   # X-Permissions dropped: not gateway
```

- [ ] **Step 6: Run to verify it fails**

- [ ] **Step 7: Implement deps.security_context**

`app/api/deps.py` — FastAPI dependency:

```python
from fastapi import Request, HTTPException

from app.config import Settings
from app.domain import security


def security_context(request: Request) -> security.SecurityContext:
    settings: Settings = request.app.state.settings
    secret = settings.auth_secret
    if not secret:
        raise HTTPException(status_code=401, detail="rag.auth_not_configured")
    token = request.headers.get("x-service-auth", "")
    try:
        claims = security.verify_service_token(token, secret, settings.service_name)
    except security.AuthenticationError:
        raise HTTPException(status_code=401, detail="rag.service_auth_required") from None
    if claims.source not in settings.trusted_sources:
        raise HTTPException(status_code=401, detail="rag.service_auth_required")
    headers = request.headers
    perms: tuple[str, ...] = ()
    if claims.source == "auth-gateway":            # only gateway has checked policy
        perms = tuple(p.strip() for p in headers.get("X-Permissions", "").split(",") if p.strip())
    return security.SecurityContext(
        tenant_id=headers.get("X-Tenant-Id") or None,
        user_id=headers.get("X-User-Id") or None,
        permissions=perms,
        source_service=claims.source,
        auth_checked=bool(headers.get("X-Auth-Checked")),
    )
```

- [ ] **Step 8: Run tests to verify they pass**

- [ ] **Step 9: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): fail-closed service-auth middleware + delegated SecurityContext"
```

---

## PHASE P2 — Ingestion pipeline

Outcome: admin can register a source, create/review/publish a version, worker indexes chunks + embeddings idempotently; job state machine correct; version only goes PUBLISHED after a fully-embedded run (atomic transaction).

## Task P2.1: Sources & versions service (CRUD + state machine)

**Files:**
- Create: `arda-be/apps/rag-service/app/domain/models.py`
- Create: `arda-be/apps/rag-service/app/db/schema.py` (SQLAlchemy Table defs)
- Create: `arda-be/apps/rag-service/app/service/sources.py`
- Create: `arda-be/apps/rag-service/app/api/sources.py`
- Test: `arda-be/apps/rag-service/tests/integration/test_sources_api.py`
- Modify: `app/main.py` (include sources router behind security_context dependency)

**Interfaces:**
- Consumes: `Settings`, `security_context`, migration tables
- Produces:
  - `service.sources.create_source(...)`, `list_sources(tenant, include_deleted=False)`, `soft_delete_source(...)`, `create_version(...)`, `list_versions(...)`, `review_version(...)`, `publish_version(...)`
  - pydantic request models: `SourceCreate`, `VersionCreate`, `ReviewRequest`
  - State machine helpers raising `DomainError` → 409 (e.g. publish when not APPROVED)
  - Routes registered under `/api/rag/sources` (admin only when auth-gateway; all require `ai.knowledge.manage` when source==auth-gateway else any trusted caller)

- [ ] **Step 1: Write domain errors + models**

`app/domain/errors.py`:

```python
class RagError(Exception):
    status_code = 400
    code = "rag.error"


class NotFoundError(RagError):
    status_code = 404
    code = "rag.not_found"


class ConflictError(RagError):
    status_code = 409
    code = "rag.conflict"


class PermissionDeniedError(RagError):
    status_code = 403
    code = "rag.forbidden"
```

`app/domain/models.py` (subset — executor fills full field list from spec §7.2):

```python
from pydantic import BaseModel, Field

class SourceCreate(BaseModel):
    tenant_id: str | None = None
    title: str = Field(min_length=1, max_length=500)
    description: str | None = None
    source_type: str = "docs"           # docs | admin | url
    scope: str = "tenant"               # tenant | global | system
    classification: str = "internal"
    language: str = "vi"
    tags: list[str] = []
    owner_id: str | None = None
    effective_from: str | None = None   # ISO-8601
    effective_to: str | None = None

class ChunkerConfig(BaseModel):
    strategy: str = "heading"
    chunk_size: int = 512
    chunk_overlap: int = 64
    chunker_version: str = "1"

class VersionCreate(BaseModel):
    version: str = Field(min_length=1, max_length=128)
    content_type: str = "markdown"      # markdown | url | file
    content: str | None = None          # when markdown
    content_url: str | None = None      # when url/file
    chunker_config: ChunkerConfig | None = None

class ReviewRequest(BaseModel):
    decision: str                        # approve | reject
    reason: str | None = None

class SourceOut(BaseModel):
    id: int
    tenant_id: str | None
    title: str
    source_type: str
    scope: str
    status: str | None
    version: str | None
    active_version_id: int | None
    ...
```

- [ ] **Step 2: Write the failing integration test**

`tests/integration/test_sources_api.py` — full admin lifecycle: create → DRAFT; create version → DRAFT; review approve → APPROVED; publish → job created + version INDEXING; worker not running → version stays INDEXING (proves the state machine doesn't skip to PUBLISHED); reject on non-APPROVED publish → 409; list excludes soft-deleted.

Use the same `_app()`/token helpers as P1.3 tests (duplicate the helper or move to `conftest.py` — prefer conftest: `tests/conftest.py` exposes `client(settings)` fixture with a working Postgres `TEST_DATABASE_DSN` and a valid gateway token; migrations already applied).

```python
def test_admin_version_lifecycle(client):
    # create source
    r = client.post("/api/rag/sources", json={"title": "Chính sách nhân sự",
                                              "scope": "tenant", "tenant_id": "tenant-a"})
    assert r.status_code == 201
    source_id = r.json()["id"]

    # create version (DRAFT)
    r = client.post(f"/api/rag/sources/{source_id}/versions",
                    json={"version": "1.0.0", "content_type": "markdown",
                          "content": "# Quy định nghỉ phép\n\nMỗi nhân viên được hưởng 12 ngày phép năm."})
    assert r.status_code == 201
    version_id = r.json()["id"]

    # reject publish before review -> 409
    r = client.post(f"/api/rag/sources/{source_id}/versions/{version_id}/publish")
    assert r.status_code == 409

    # review approve -> APPROVED
    r = client.post(f"/api/rag/sources/{source_id}/versions/{version_id}/review",
                    json={"decision": "approve"})
    assert r.status_code == 200
    assert r.json()["status"] == "APPROVED"

    # publish -> job + INDEXING
    r = client.post(f"/api/rag/sources/{source_id}/versions/{version_id}/publish")
    assert r.status_code == 202
    job_id = r.json()["job_id"]
    r = client.get(f"/api/rag/jobs/{job_id}")
    assert r.status_code == 200
    assert r.json()["status"] in ("pending", "processing")
```

- [ ] **Step 3: Run to verify it fails**

- [ ] **Step 4: Implement db/schema.py** — SQLAlchemy `Table` objects (autoload from the migrated DB is fragile; define columns explicitly mirroring the SQL DDL).

- [ ] **Step 5: Implement service/sources.py + api/sources.py** with the state machine:

State transitions (spec §3.2):
- `create_source` → row, `status='DRAFT'`? No — sources have no own status in rag spec beyond `deleted_at`; versions carry state. Sources always "active" unless soft-deleted.
- `create_version`: insert `ai_knowledge_source_versions(status='DRAFT', content_hash=sha256(content))`, enforce `UNIQUE(source_id, version)` → 409 on duplicate
- `review`: only `DRAFT→APPROVED` (approve) or `DRAFT→DRAFT` w/ reason (reject → stays DRAFT, records reason in status_history JSONB); any other transition → 409
- `publish`: only from `APPROVED`; insert `ai_ingestion_jobs(status='pending', source_version_id, max_attempts=3)`; `version.status='INDEXING'`; return `job_id`
- Content for `url`/`file` types: content_url stored; the actual fetch is the worker's concern (P2.4) — Phase 1 supports `markdown` inline only; `url`/`file` return 501 (`rag.not_supported_yet`)

Versioned status_history: append `{"to": "APPROVED", "at": iso, "by": user_id, "reason": ...}`.

- [ ] **Step 6: Run tests to verify they pass**

- [ ] **Step 7: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): sources/versions CRUD + review→publish state machine"
```

## Task P2.2: Chunker (heading-based, with overlap + content_hash)

**Files:**
- Create: `arda-be/apps/rag-service/app/rag/chunker.py`
- Test: `arda-be/apps/rag-service/tests/unit/test_chunker.py`

**Interfaces:**
- Consumes: `VersionCreate.chunker_config`
- Produces: `rag.chunker.chunk_markdown(markdown: str, *, chunk_size: int = 512, chunk_overlap: int = 64, chunker_version: str = "1") -> list[Chunk]` where `Chunk = dataclass(heading: str, content: str, content_hash: str)`
- Chunk idempotency input: `content_hash` per chunk = sha256 of the chunk's text

Chunking rules (mirror the Go `Chunk` in `ai-service/internal/knowledge/indexer.go`, plus overlap and `chunk_id`):
- First `# ` line → document title (handled by caller, not chunked)
- Split on `##`/`###` headings; body = lines until next heading
- Oversized body split on blank-line paragraph boundaries with **overlap** — last `chunk_overlap` words (or chars) of the previous part prefixed to the next part
- Trim whitespace; drop empty bodies

- [ ] **Step 1: Write failing test**

```python
from app.rag.chunker import chunk_markdown


def test_heading_chunks_and_drops_title():
    md = "# Chính sách nhân sự\n\n## Nghỉ phép\n\nMỗi năm 12 ngày.\n\n### Tang lễ\n\n3 ngày.\n"
    chunks = chunk_markdown(md)
    assert [c.heading for c in chunks] == ["Nghỉ phép", "Tang lễ"]
    assert all(c.content_hash for c in chunks)
    assert "Chính sách nhân sự" not in chunks[0].heading


def test_oversized_body_is_split_with_overlap():
    md = "# T\n\n## A\n\n" + "\n\n".join(f"Đoạn {i}: " + "nội dung dài. " * 60 for i in range(10)) + "\n"
    chunks = chunk_markdown(md, chunk_size=120, chunk_overlap=20)
    assert len(chunks) > 1
    assert "nội dung dài." in chunks[-1].content
```

- [ ] **Step 2: Run to verify it fails**

- [ ] **Step 3: Implement chunker.py**

```python
import hashlib
import re
from dataclasses import dataclass


@dataclass(frozen=True)
class Chunk:
    heading: str
    content: str
    content_hash: str


def _sha256(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def _words(text: str) -> list[str]:
    return re.findall(r"\S+", text, flags=re.UNICODE)


def chunk_markdown(markdown: str, *, chunk_size: int = 512,
                   chunk_overlap: int = 64, chunker_version: str = "1") -> list[Chunk]:
    """Heading-scoped chunker (Go indexer behavior) with overlap for oversized bodies."""
    chunks: list[Chunk] = []
    for heading, body in _split_by_headings(markdown):
        body = body.strip()
        if not body:
            continue
        for part in _split_with_overlap(body, chunk_size, chunk_overlap):
            chunks.append(Chunk(heading=heading.strip(), content=part, content_hash=_sha256(part)))
    return chunks


def _split_by_headings(markdown: str):
    """Yield (heading, body) pairs. '# ' title lines are skipped."""
    lines = markdown.replace("\r\n", "\n").split("\n")
    current_heading, current_body, title_seen = "", [], False
    for line in lines:
        if re.match(r"^# ", line) and not title_seen:
            title_seen = True
            continue
        if re.match(r"^#{2,3} ", line):
            if current_body:
                yield current_heading, "\n".join(current_body)
            current_heading = re.sub(r"^#+\s*", "", line)
            current_body = []
        else:
            current_body.append(line)
    if current_body:
        yield current_heading, "\n".join(current_body)


def _split_with_overlap(body: str, chunk_size: int, chunk_overlap: int) -> list[str]:
    words = _words(body)
    if len(words) <= chunk_size:
        return [body]
    paragraphs = [p.strip() for p in body.split("\n\n") if p.strip()]
    parts: list[str] = []
    current: list[str] = []
    count = 0
    for para in paragraphs:
        p_words = _words(para)
        if count and count + len(p_words) > chunk_size:
            tail = " ".join(_words(" ".join(current))[-chunk_overlap:]) if chunk_overlap else ""
            parts.append(" ".join(current))
            current = [tail] if tail else []
            count = len(current and _words(current[0])) if current else 0
        current.append(para)
        count += len(p_words)
    if current:
        parts.append(" ".join(current))
    return [p for p in parts if p]
```

(The executor may refine `_split_with_overlap` — the invariant to test is: no part exceeds chunk_size words, consecutive parts share the overlap tail, and content_hash is sha256 of the exact stored content.)

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): heading chunker with overlap and per-chunk content hash"
```

## Task P2.3: Embedder wrapper + dimension guard

**Files:**
- Create: `arda-be/apps/rag-service/app/rag/embedder.py`
- Test: `arda-be/apps/rag-service/tests/unit/test_embedder.py`

**Interfaces:**
- Consumes: `Settings.embedding`
- Produces:
  - `class EmbeddingError(Exception)`
  - `rag.embedder.Embedder` — dataclass wrapping `llama_index.embeddings.openai.OpenAIEmbedding` with `.embed(texts: list[str]) -> list[list[float]]`, `.model`, `.dimensions`
  - Raises `EmbeddingError` on dimension mismatch vs `Settings.embedding.dimensions`
  - Factory `build_embedder(settings) -> Embedder | None` (None when not configured)

- [ ] **Step 1: Write the failing test (mock LLM — never hit Cloudflare in tests)**

`tests/unit/test_embedder.py` — monkeypatch the LlamaIndex client so `.embed()` returns 1023-dim vectors and assert `EmbeddingError` raised; 1024-dim vectors pass.

```python
import pytest

from app.rag.embedder import Embedder, EmbeddingError, build_embedder


class _FakeClient:
    def __init__(self, dim: int) -> None:
        self.dim = dim

    def embeddings(self, params=None, **kw):
        del params, kw
        n = len(self._texts)  # type: ignore[attr-defined]
        return [{"embedding": [0.1] * self.dim, "index": i} for i in range(n)]


def test_dimension_mismatch_raises(monkeypatch):
    ...
    assert isinstance(cm.value, EmbeddingError)
```

(Implementation detail: `OpenAIEmbedding` from llama-index-embeddings-openai is called with `api_base`, `api_key`, `model`, `embed_batch_size`; its sync `.get_text_embedding_batch` is what the worker calls. The wrapper passes through; the unit test fakes the HTTP layer by subclassing and overriding the embed call, or the wrapper accepts an injectable `call`.)

- [ ] **Step 2: Run to verify it fails**

- [ ] **Step 3: Implement embedder.py**

```python
from dataclasses import dataclass

from app.config import EmbeddingSettings


class EmbeddingError(Exception):
    pass


@dataclass
class Embedder:
    settings: EmbeddingSettings
    _client: object | None = None   # OpenAIEmbedding instance (lazy)

    @property
    def model(self) -> str:
        return self.settings.model

    @property
    def dimensions(self) -> int:
        return self.settings.dimensions

    def _client_impl(self):
        if self._client is None:
            from llama_index.embeddings.openai import OpenAIEmbedding
            api_key = _env(self.settings.api_key_env)
            self._client = OpenAIEmbedding(
                model=self.settings.model,
                api_key=api_key,
                api_base=self.settings.base_url or None,
                embed_batch_size=self.settings.batch_size,
            )
        return self._client

    def embed(self, texts: list[str]) -> list[list[float]]:
        if not texts:
            return []
        vectors = self._client_impl().get_text_embedding_batch(texts)
        for vector in vectors:
            if len(vector) != self.settings.dimensions:
                raise EmbeddingError(
                    f"Dimension mismatch: expected {self.settings.dimensions}, got {len(vector)}"
                )
        return vectors


def build_embedder(settings: EmbeddingSettings) -> Embedder | None:
    if not settings.base_url or not _env(settings.api_key_env):
        return None
    return Embedder(settings)
```

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): OpenAI-compatible embedder wrapper with dimension guard"
```

## Task P2.4: Durable queue + worker

**Files:**
- Create: `arda-be/apps/rag-service/app/db/queue.py`
- Create: `arda-be/apps/rag-service/app/service/ingestion.py`
- Create: `arda-be/apps/rag-service/app/worker.py`
- Test: `arda-be/apps/rag-service/tests/integration/test_queue_worker.py`

**Interfaces:**
- Consumes: chunker, embedder, `Settings.worker`, tables
- Produces:
  - `db.queue.claim_job(conn, worker_id, lease_sec) -> Job | None` (SKIP LOCKED, honors `next_retry_at`, bumps attempts)
  - `db.queue.heartbeat(conn, job_id) -> None` (refreshes `locked_at`)
  - `db.queue.complete_job(conn, job_id, version_id, source_id) -> None` — **atomic transaction** (job completed + version PUBLISHED + source.active_version_id) with rowcount checks → rollback on mismatch
  - `db.queue.fail_job(conn, job_id, error, version_id, attempts, max_attempts, backoff_sec) -> None` — sets job failed + version FAILED (when attempts exhausted) or requeues with `next_retry_at`
  - `db.queue.reset_expired_leases(conn, lease_sec) -> int` — lease expiry → back to pending
  - `app.worker.run_worker(settings, *, once: bool = False)` — CLI entry `python -m app.worker` (loop) / `python -m app.worker --once` (test)

Worker flow per job (spec §4.2):
```
1 claim → 2 chunk (chunk_id = sha256(source_version_id + chunk_index + content_hash + chunker_version))
        → INSERT ... ON CONFLICT (chunk_id) DO NOTHING (total_chunks=inserted+existing)
        → resume: SELECT chunk_id FROM chunks WHERE source_version_id=? AND embedding IS NULL
        → 3 embed missing (batch 16, retry 500ms→2s→8s, 3 attempts; dimension guard)
        → UPDATE chunks SET embedding, embedding_model WHERE chunk_id
        → 4 heartbeat every 60s while running
        → 5 complete_job (atomic) → 6 FAILED on attempts exhausted / retry on transient
```

- [ ] **Step 1: Write the failing integration test**

`tests/integration/test_queue_worker.py` — needs Postgres. Seed: create source+version (APPROVED), publish → job. Run `run_worker(settings, once=True)` with a stub embedder (deterministic 1024-dim vectors via the fake client). Assert: version PUBLISHED, source.active_version_id set, chunks all have embedding, job completed. Second scenario: embedder that always raises → after `max_attempts`, job failed + version FAILED + active_version_id untouched. Third: worker dies mid-job (simulate by claiming with a lease in the past) → `reset_expired_leases` → claimable again → idempotent re-run (no duplicate chunk rows).

```python
def test_worker_indexes_and_publishes(client, settings):  # conftest gives a live app+db
    # create + review + publish a version (as in P2.1 test)
    ...
    from app.worker import run_worker
    run_worker(settings, once=True)   # with stub embedder injected via settings or monkeypatch

    r = client.get(f"/api/rag/sources/{source_id}/versions/{version_id}")
    assert r.json()["status"] == "PUBLISHED"
    r = client.get(f"/api/rag/sources/{source_id}")
    assert r.json()["active_version_id"] == version_id
```

- [ ] **Step 2: Run to verify it fails**

- [ ] **Step 3: Implement queue.py (SKIP LOCKED claim + lease)**

```python
# claim_job: exact SQL from spec §4.2
UPDATE ai_ingestion_jobs AS j
   SET status = 'processing', locked_by = :worker, locked_at = now(),
       attempts = attempts + 1
 WHERE j.id = (
     SELECT id FROM ai_ingestion_jobs
      WHERE status = 'pending' AND attempts < max_attempts
        AND (next_retry_at IS NULL OR next_retry_at <= now())
      ORDER BY created_at ASC
      LIMIT 1
      FOR UPDATE SKIP LOCKED
 )
RETURNING id, source_version_id, attempts, max_attempts;
```

`complete_job` — atomic 3-update transaction with rowcount checks (spec §3.2):

```python
with engine.begin() as conn:
    job = conn.execute(text(
        "UPDATE ai_ingestion_jobs SET status='completed', updated_at=now()"
        " WHERE id=:job AND status='processing' RETURNING id"
    ), {"job": job_id}).fetchone()
    ver = conn.execute(text(
        "UPDATE ai_knowledge_source_versions SET status='PUBLISHED', updated_at=now()"
        " WHERE id=:vid AND source_id=:sid AND status='INDEXING' RETURNING id"
    ), {"vid": version_id, "sid": source_id}).fetchone()
    src = conn.execute(text(
        "UPDATE ai_knowledge_sources SET active_version_id=:vid, updated_at=now()"
        " WHERE id=:sid RETURNING id"
    ), {"vid": version_id, "sid": source_id}).fetchone()
    if not (job and ver and src):
        raise RuntimeError("atomic publish failed — rowcount mismatch")  # → whole tx rolls back
```

- [ ] **Step 4: Implement ingestion.py (chunk + embed + persist) and worker.py**

`service/ingestion.py`:

```python
def process_job(engine, job, embedder, chunker_version) -> None: ...
# reads version row (content, chunk_size, chunk_overlap, chunker_version)
# chunk_markdown → for i, chunk: chunk_id = sha256(f"{version_id}:{i}:{chunk.content_hash}:{chunker_version}")
# insert on conflict do nothing; count rows needing embedding (embedding IS NULL)
# embed in batches of settings.embedding.batch_size; dimension guard raises EmbeddingError → job retry/fail
# update embedding, embedding_model where chunk_id
# total_chunks/embedded_chunks counters on the job
```

`app/worker.py`:

```python
import argparse, logging, time
from app.config import Settings
from app.db.engine import get_engine
from app.db import queue
from app.service import ingestion

def run_worker(settings: Settings, *, once: bool = False) -> None:
    engine = get_engine(settings)
    worker_id = f"worker-{uuid4().hex[:8]}"
    embedder = ...  # build_embedder(settings.embedding)
    while True:
        with engine.begin() as conn:
            queue.reset_expired_leases(conn, settings.worker.lease_duration_sec)
            job = queue.claim_job(conn, worker_id, settings.worker.lease_duration_sec)
        if job is None:
            if once: return
            time.sleep(settings.worker.poll_interval_sec); continue
        try:
            ingestion.process_job(engine, job, embedder)
            with engine.begin() as conn:
                queue.complete_job(conn, job.id, job.version_id, job.source_id)
        except Exception as exc:            # noqa: BLE001 — job-level failure handling
            logger.exception("job %s failed", job.id)
            with engine.begin() as conn:
                queue.fail_or_requeue(conn, job.id, str(exc), settings.worker)
        if once: return
```

- [ ] **Step 5: Run tests to verify they pass**

- [ ] **Step 6: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): durable Postgres queue (SKIP LOCKED) + ingestion worker"
```

## Task P2.5: Jobs API + review/publish wiring into ingestion

**Files:**
- Create: `arda-be/apps/rag-service/app/api/jobs.py`
- Create: `arda-be/apps/rag-service/app/service/feedback.py` (feedback table insert, used by P5 too)
- Modify: `app/main.py` — register `/api/rag/jobs/{job_id}` GET + `/api/rag/feedback` POST (both behind security_context)

**Interfaces:**
- Consumes: queue.py
- Produces: `GET /api/rag/jobs/{id}` → `{id, source_version_id, status, attempts, max_attempts, error_message, total_chunks, embedded_chunks, next_retry_at}`; `POST /api/rag/feedback` `{run_id, helpful, comment}` → 201

- [ ] **Step 1: Write failing test** (job poll after publish returns row; feedback requires existing run → 404 on unknown run_id — run_id FK validated)

- [ ] **Step 2: Run to verify it fails** … **Step 3: implement** … **Step 4: pass** …

- [ ] **Step 5: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): jobs poll + feedback endpoints"
```

---

## PHASE P3 — Query pipeline

Outcome: POST /api/rag/query returns tenant-scoped, permission-filtered, reranked hits with citations, and records an `ai_rag_runs` row.

## Task P3.1: Hybrid retriever (FTS + pgvector + RRF)

**Files:**
- Create: `arda-be/apps/rag-service/app/rag/retriever.py`
- Test: `arda-be/apps/rag-service/tests/integration/test_retriever.py`

**Interfaces:**
- Consumes: `Settings.retrieval`, engine, embedder
- Produces:
  - `@dataclass Candidate: chunk_id: str, source_id: int, source_version_id: int, version: str, title: str, heading: str, content: str, score: float, source: str  # "vector" | "fts"`
  - `retriever.hybrid_search(conn, query: str, query_vector: list[float] | None, *, tenant_id: str | None, settings: RetrievalSettings) -> list[Candidate]` (RRF-fused, deduped, ≤ rerank_candidates)
- SQL filter (spec §5.3, non-negotiable):
```sql
WHERE s.tenant_id IS NOT DISTINCT FROM :tenant          -- NULL tenant = global/system
  AND s.scope IN ('tenant', 'global')                   -- system never exposed
  AND s.active_version_id = v.id
  AND v.status = 'PUBLISHED'
  AND (s.effective_from IS NULL OR s.effective_from <= now())
  AND (s.effective_to   IS NULL OR s.effective_to   > now())
  AND s.deleted_at IS NULL
```
Vector leg: `WHERE c.embedding IS NOT NULL AND c.embedding_model = :model ORDER BY c.embedding <=> :vec LIMIT vector_top_k` with `tenant+scope+active_version` filter applied. FTS leg: PostgreSQL `to_tsvector('simple', …) @@ plainto_tsquery('simple', :q)` on content (+ title). RRF: `score = Σ 1/(60 + rank)`, dedupe by chunk_id, keep top `rerank_candidates`.

- [ ] **Step 1: Write the failing integration test** — seed two sources: `tenant-a` FAQ về nghỉ phép + `global` policy; query from tenant-a must return both but **never** a `system` source nor a tenant-b source; vector leg gated on `embedding_model`; un-embedded chunks retrievable via FTS only.

- [ ] **Step 2–4: TDD loop** … **Step 5: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): hybrid FTS+pgvector retriever with tenant/permission SQL filter and RRF"
```

## Task P3.2: Reranker abstraction (none + anthropic)

**Files:**
- Create: `arda-be/apps/rag-service/app/rag/reranker.py`
- Test: `arda-be/apps/rag-service/tests/unit/test_reranker.py`

**Interfaces:**
- Consumes: `Settings.reranker`
- Produces:
  - `class Reranker(Protocol)` — `rerank(query, candidates, top_n) -> list[Candidate]` (spec §5.2)
  - `NoneReranker.rerank` returns candidates[:top_n] (provider `none`)
  - `AnthropicReranker.rerank` — calls Haiku with a scoring prompt; returns top_n. Guarded: `timeout_ms`, mockable client injection; on LLM error → fall back to input order (never fail the query because rerank failed)
  - `build_reranker(settings) -> Reranker`

- [ ] **Step 1: failing test** — NoneReranker slices; AnthropicReranker with injected fake client returns the order the fake returns, and on exception returns input order unchanged (fallback).

- [ ] **Step 2–4: TDD loop** … **Step 5: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): reranker protocol — none + anthropic impl with fallback"
```

## Task P3.3: Query pipeline service + /query endpoint

**Files:**
- Create: `arda-be/apps/rag-service/app/service/query_service.py`
- Create: `arda-be/apps/rag-service/app/api/query.py`
- Test: `arda-be/apps/rag-service/tests/integration/test_query_api.py`
- Modify: `app/main.py` — register router

**Interfaces:**
- Produces: `POST /api/rag/query` request `{query: str (≤512), top_k: int (1..10)}` → response per spec §7.1 (`run_id`, `hits[]`, `latency_ms`, `rewritten=false`, `retrieved_count`, `reranked_count`)
- Inserts `ai_rag_runs` (id, tenant_id, query, retrieved_count, reranked_count, hit_ids, latency_ms, model_used)
- `hits[].citation = f"[{source_id}:{heading}]"`

Pipeline order (spec §6.1 invariant): SecurityContext → SQL-filtered retrieval (vector + fts) → RRF → rerank(top_k) → top_k → citation projection. **Never** pass raw FTS/vector scores to the model; only citation + content.

- [ ] **Step 1: failing test** — with `reranker.provider=none`, seeded tenant-a docs: query "cách tính phép năm" returns hits with tenant-a + global docs only; `top_k=0` and `top_k=11` → 422; empty query → 422; response has `run_id`, `retrieved_count`, `reranked_count`; a row exists in ai_rag_runs for the returned run_id; no `X-Tenant-Id` header (or body tenant) can widen scope — call with a body-less forged tenant and assert tenant filter still applies.

- [ ] **Step 2–4: TDD loop** … **Step 5: Commit**

```bash
git add apps/rag-service/
git commit -m "feat(rag): /query pipeline — security filter → RRF → rerank → citations, run traced"
```

## Task P3.4: Query-rewrite placeholder (off by default)

**Files:**
- Modify: `app/rag/pipeline.py` — add `rewrite_enabled: bool = False` to pipeline config and a `None` rewrite step. No LLM rewrite in Phase 1. Tests assert `rewritten: false` always.

(No code beyond the flag + `rewritten` field; implement the optional LLM rewrite only after eval data says queries need it.)

- [ ] **Step 1: test asserts `rewritten == false` for a query containing ambiguous words** … commit.

```bash
git commit -m "feat(rag): query rewrite stub — disabled by default, rewritten:false contract"
```

---

## PHASE P4 — ai-service integration

Outcome: Olorin knowledge lookup goes through rag-service; ai-service's own `knowledge.*` code paths are removed or bypassed; gateway policy routes `/api/rag/*` to rag-service.

## Task P4.1: RagClient in ai-service svcclient (signed delegated requests)

**Files:**
- Create: `arda-be/apps/ai-service/internal/svcclient/rag.go`
- Modify: `arda-be/apps/ai-service/internal/config/config.go` (add `RAGServiceURL string`)
- Modify: `arda-be/apps/ai-service/cmd/ai-service/main.go` (wire URL into the client when enabling read tools)
- Test: `arda-be/apps/ai-service/internal/svcclient/rag_test.go`

**Interfaces:**
- Consumes: `svcclient.Client` (transport.go), `metadata.Context`
- Produces: `svcclient.NewRAGClient(baseURL, source, secret string, hc *http.Client) *RAGClient` with
  - `Search(ctx, md metadata.Context, query string, topK int) ([]RAGHit, error)` — POST `/api/rag/query`, body `{"query":…, "top_k":…}` (per spec §7.1), headers via `Client.NewRequest`
  - `RAGHit` fields: `SourceID`, `SourceVersionID`, `Version`, `Title`, `Heading`, `Content`, `Score`, `Citation`
  - Returns `run_id` too (callers log it for feedback)
- Security: `NewRequest` already signs + forwards X-Tenant-Id/X-User-Id — body must NOT carry identity (it doesn't)

- [ ] **Step 1: failing test (httptest server asserting request shape)**
```go
// assert Authorization header present? No — assert x-service-auth present,
// X-Tenant-Id forwarded, body JSON == {"query": "phép năm", "top_k": 3}
```

- [ ] **Step 2–4: TDD loop** … **Step 5: Commit**

```bash
cd arda-be
git add apps/ai-service/
git commit -m "feat(ai): svcclient RAG client — signed /api/rag/query calls"
```

## Task P4.2: Route knowledge.search → rag-service in the sandbox catalog

**Files:**
- Modify: `arda-be/apps/ai-service/internal/catalog/knowledge.go` — replace the `searcher knowledge.Searcher` SQL path with the RAG client call
- Modify: `arda-be/apps/ai-service/internal/catalog/suite.go` + `builtins.go` — construct RAGClient, drop SQLSearcher/embedder wiring
- Modify: `arda-be/apps/ai-service/cmd/ai-service/main.go` — drop `buildKnowledgeEmbedder`, pass RAG client into the suite
- Modify: `arda-be/apps/ai-service/internal/knowledge/*` — keep `Searcher` interface (rag client satisfies it) or delete search.go/indexer.go/embedder.go (they die with the docs-as-code path)
- Modify: `arda-be/apps/ai-service/internal/config/config.go` — remove now-dead embedding/knowledge flags? Keep for one release, but RAGServiceURL is authoritative

Decision (with user): which parts of `internal/knowledge/*` survive?
- `Hit` shape is reused by the rag client (rename to `RAGHit`)
- `SQLSearcher`, `Indexer`, `embedder.go` are deleted once rag-service owns ingestion
- `cmd/knowledge-indexer` binary: deleted (CI that ran it against docs-as-code → rag-service admin API)

- [ ] **Step 1: failing test** — catalog test with a fake RAGClient asserts `knowledge.search` hits `RAGClient.Search` with query + top_k and returns the documented shape (`citations`, `matchScore` non-null now — this was the previous bug).

- [ ] **Step 2–4: TDD loop** … **Step 5: Commit**

```bash
git commit -m "feat(ai): knowledge.search now calls rag-service (RAG) — remove SQL search path"
```

## Task P4.3: Scale-down Go ai-service knowledge writes; run rag migrations against prod DB

**Pre-flight checklist (run in order):**
1. [ ] Scale ai-service to 0 (`kubectl scale deployment ai-service --replicas=0`) — its migrations and the Go tables conflict with rag ownership
2. [ ] Back up the knowledge tables (`pg_dump -t ai_knowledge_sources -t ai_knowledge_chunks`)
3. [ ] Apply rag migrations (rag-api rollout) — reconcile legacy rows
4. [ ] Smoke: index a docs source through rag admin API → PUBLISHED
5. [ ] Smoke: `knowledge.search` through the agent returns citations

(Deployment manifests for rag-api/rag-worker live in arda-infra — separate repo workstream, not this plan.)

## Task P4.4: auth-gateway policy + permission for /api/rag/*

**Files:**
- Modify: `arda-be/apps/auth-gateway/configs/policy.yaml`
- Test: check-security-invariants.mjs / check-ai-catalog.mjs style — routes must be declared in policy.yaml

Add to policy.yaml:

```yaml
  - id: rag-query
    path: /api/rag/query
    methods: [POST]
    auth: true
    risk: low
    permissions:
      - ai.assistant.use

  - id: rag-sources-read
    path: /api/rag/sources/**
    methods: [GET]
    auth: true
    risk: medium
    permissions:
      - ai.admin
      - superadmin
      - platform.manage

  - id: rag-sources-write
    path: /api/rag/sources/**
    methods: [POST, PUT, PATCH, DELETE]
    auth: true
    risk: high
    permissions:
      - ai.admin
      - superadmin
      - platform.manage
```

Note: query is **service-to-service** (ai-service → rag-service directly, bypassing gateway). The policy above is only needed if the gateway also proxies rag; if direct service calls only, register rag routes in gateway only for admin UI (P5). Clarify with infra owner.

- [ ] **Step 1: verify CI** — `node scripts/check-security-invariants.mjs`, `node scripts/check-ai-catalog.mjs` … commit.

```bash
git commit -m "feat(auth): policy routes for /api/rag/* (admin) + rag-query service route"
```

---

## PHASE P5 — Eval harness + admin UI + feedback loop

Outcome: RAGAS eval script offline; admin UI (MFE) to manage sources/versions; feedback from chat wired to ai_rag_feedback.

## Task P5.1: RAGAS eval harness (offline script)

**Files:**
- Create: `arda-be/apps/rag-service/scripts/eval_ragas.py`
- Create: `arda-be/apps/rag-service/scripts/eval_set.json` (sample ~10 questions per domain from `ai_rag_eval` seeds)
- Test: `tests/unit/test_eval_parsing.py` (dataset schema validation only — metrics run offline, not in CI)

**Interfaces:**
- Consumes: query pipeline + `ai_rag_eval` rows
- Produces: console report: faithfulness, context precision, context recall, answer relevancy (RAGAS) per domain + overall
- Gates NOT hard-coded (spec §8.3) — threshold config flags `--min-faithfulness`, `--min-context-recall` default off; P4 switch gate uses calibrated values

- [ ] **Step 1: TDD on the dataset parsing** … **Step 5: commit**

```bash
git commit -m "feat(rag): offline RAGAS eval harness over ai_rag_eval"
```

## Task P5.2: Feedback loop — chat UI thumbs → rag feedback

**Files (MFE repo — arda-mfe, NOT arda-be):**
- Modify: `arda-mfe` chat message component — add 👍/👎 on assistant RAG answers (answer carries run_id via ai-service response extension)
- Modify: ai-service agent response to include `run_id` in the tool result metadata (ai_conversations/messages content_json), so the UI knows which run to rate
- Modify: ai-service — POST /api/rag/feedback with delegated identity on thumbs-down + comment

## Task P5.3: Admin UI (MFE) for knowledge management

**Files (MFE repo):** knowledge page: source list (tenant + global scope), create source, version list per source, review/publish buttons, job status poll. Calls auth-gateway `/api/rag/sources*`.

---

## Cross-phase checklists

### Every commit: run before push (P1–P5, all repos)
```bash
cd arda-be
node scripts/check-migrations.mjs        # rag migrations must pass the naming/marker rules
node scripts/check-security-invariants.mjs
node scripts/check-secrets.mjs           # no secrets in pyproject/README/migrations
git diff --check
```

### Rag-service selfcheck (run locally each phase)
```bash
cd apps/rag-service
python -m pytest tests/ -q
python scripts/selfcheck.py
```

### Security checklist (every endpoint added)
- [ ] Protected by `security_context` dependency (fail-closed 401)
- [ ] Request body carries zero identity fields
- [ ] Tenant/scope SQL filter present on every retrieval query
- [ ] X-Permissions honored only when source == auth-gateway
- [ ] Logs never include full query text outside ai_rag_runs
- [ ] top_k clamped server-side (1..10)

### Migration safety checklist (P1)
- [ ] Filenames match `^\d{14}_…\.sql$`
- [ ] `-- +goose Up` / `-- +goose Down` markers present
- [ ] No `DROP … CASCADE` in new migrations
- [ ] Advisory lock around startup runner
- [ ] Backfill/rename legacy ai-service tables handled explicitly (P4.3 pre-flight)

---

## Spec coverage map (self-review)

| Spec section | Plan tasks |
|---|---|
| §2.4 endpoints | P2.1 (sources/versions), P2.5 (jobs/feedback), P3.3 (query), P5.3 (admin UI routes) |
| §3.1–3.7 schema | P1.2 |
| §3.8 migration lock | P1.2 |
| §4.1 worker/API split | P1.1 (Dockerfile command), P2.4 (worker) — deployment split is infra workstream |
| §4.2 job lifecycle + SKIP LOCKED + next_retry_at | P2.4 |
| §4.3 lease/heartbeat | P2.4 |
| §4.4 dimension guard | P2.3 |
| §4.5 embedding immutable contract | P2.3 + Global Constraints |
| §5.1 pipeline + rewrite-off | P3.1–P3.4 |
| §5.2 reranker protocol | P3.2 |
| §5.3 metadata filter | P3.1 |
| §6 security invariant | P1.3 + P3.3 + every-task security checklist |
| §7.1 query API + top_k bounds | P3.3 |
| §7.2 sources/versions version-centric | P2.1 |
| §7.3 jobs/feedback | P2.5 |
| §8 eval RAGAS + gates | P5.1 |
| §9 rollout phases | this plan's P1–P5 mapping |
| §10 observability + retention | P3.3 (ai_rag_runs) + P5.2 (feedback); retention policy deferred to data-governance (explicitly not hard-coded) |
| §11 deployment manifests | Infra workstream (arda-infra), called out in P4.3/P4.4 |
| §12 folder structure | File Structure Map |
| §13 dependency pins | P1.1 |

## Out of scope (explicitly deferred — add when data says so)
- Query rewrite LLM (off by default; revisit after eval baseline)
- url/file content_type ingestion (501 stub in P2.1)
- cross_encoder reranker (config value exists; impl future)
- RAGAS thresholds hard-coding (calibrate after baseline)
- data retention cron (with data-governance)
- Deployment/HPA/Argo manifests (arda-infra repo)
- ai-service `internal/knowledge` deletion — see P4.2 decision point
