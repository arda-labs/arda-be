# Rag-service: Python RAG service with LlamaIndex — Design Spec

Status: **Draft for review — implementation contract**
Ngày: 2026-09-03
Repo: `arda-be` (apps/rag-service)

## 1. Tóm tắt

`ai-service` (Go) hiện làm chatbot Olorin với knowledge retrieval thô sơ:
chunking theo heading, embedding `qwen3-embedding-0.6b` yếu cho tiếng Việt,
không query rewriting, không rerank, không eval. Spec này định nghĩa
**`rag-service`** — microservice Python thứ 12 của `arda-be`, sở hữu toàn bộ
ingestion + retrieval + quản lý kho kiến thức, xây trên **LlamaIndex**,
**pgvector** (dùng chung Postgres hiện có), **FTS + vector hybrid với RRF**,
**LLM reranker** và **Postgres-backed durable queue**.

Quyết định cốt lõi (đã chốt): **không** thêm Qdrant/Redis/NATS/Kafka ở Phase 1.
Postgres + pgvector + FTS + RRF + LLM reranker + Postgres queue là đủ và đúng
tầm.

## 2. Vị trí & kiến trúc service

### 2.1 Vị trí

`arda-be/apps/rag-service/` — cạnh ai-service, tuân theo CI/cấu trúc chung của
monorepo (11 Go microservice hiện có → 1 Go + 1 Python).

### 2.2 Công nghệ

- Python 3.13 + FastAPI + uvicorn
- `llama-index-core`, `llama-index-vector-stores-postgres`,
  `llama-index-embeddings-openai` (dùng **OpenAI-compatible endpoint** để gọi
  Cloudflare Workers AI embedding)
- SQLAlchemy 2.0 + **psycopg v3**
- Anthropic SDK cho LLM reranker (config-driven abstraction)
- HTTP REST nội bộ — pattern `svcclient` sẵn có của ai-service

### 2.3 Vai trò & ownership

rag-service **sở hữu hoàn toàn** ingestion + retrieval + quản lý kho kiến thức.
ai-service chỉ còn là orchestrator (agent loop) gọi vào.

```
text
auth-gateway → ai-service (Go, agent loop)
                 │  HTTP /api/rag/query (service-to-service, delegated identity)
                 ▼
              rag-service (Python, LlamaIndex pipeline)
                 │  Postgres (pgvector)
                 ▼
              ai_knowledge_* tables (dùng chung DB hiện tại)
```

### 2.4 Endpoints

| Method | Path | Mô tả |
|---|---|---|
| POST | `/api/rag/query` | RAG query pipeline (retrieval + rerank + citations) |
| POST | `/api/rag/sources` | Đăng ký nguồn kiến thức (admin) |
| GET | `/api/rag/sources` | Danh sách (admin UI) |
| DELETE | `/api/rag/sources/{source_id}` | Soft-delete |
| POST | `/api/rag/sources/{source_id}/versions` | Tạo version (content: markdown/file/url) |
| GET | `/api/rag/sources/{source_id}/versions` | Danh sách version |
| POST | `/api/rag/sources/{source_id}/versions/{version_id}/review` | Review gate |
| POST | `/api/rag/sources/{source_id}/versions/{version_id}/publish` | Publish → tạo ingestion job |
| GET | `/api/rag/jobs/{job_id}` | Poll job status |
| POST | `/api/rag/feedback` | User feedback gắn `run_id` |

### 2.5 Bảo mật

Nội bộ — validate service-to-service auth (HMAC signed, reuse
`arda-grpc/identity` contract, xem §6). Mọi request admin qua auth-gateway
policy (`policy.yaml`) với permission `ai.knowledge.manage`.

## 3. DB Schema (framework-independent, trên Postgres hiện có)

Schema tự định nghĩa — **không phụ thuộc auto-migration của LlamaIndex**.
LlamaIndex chỉ dùng qua code (vector store qua code, không qua migration của nó).

### 3.1 `ai_knowledge_sources` — metadata của nguồn

```sql
CREATE TABLE IF NOT EXISTS public.ai_knowledge_sources (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       TEXT,
  title           TEXT NOT NULL,
  description     TEXT,
  source_type     TEXT NOT NULL DEFAULT 'docs',   -- docs | admin | url
  scope           TEXT NOT NULL DEFAULT 'tenant', -- tenant | global | system
  classification  TEXT NOT NULL DEFAULT 'internal',
  language        TEXT DEFAULT 'vi',
  tags            TEXT[] DEFAULT '{}',
  owner_id        TEXT,
  effective_from  TIMESTAMPTZ,   -- nguồn chỉ retrievable trong hiệu lực
  effective_to    TIMESTAMPTZ,
  active_version_id BIGINT REFERENCES ai_knowledge_source_versions(id),  -- version đang phục vụ query; khi publish đảm bảo version.source_id = source.id
  deleted_at      TIMESTAMPTZ,   -- soft-delete
  created_by      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.2 `ai_knowledge_source_versions` — version + state machine + chunking config

```sql
CREATE TABLE IF NOT EXISTS public.ai_knowledge_source_versions (
  id              BIGSERIAL PRIMARY KEY,
  source_id       BIGINT NOT NULL REFERENCES ai_knowledge_sources(id) ON DELETE CASCADE,
  version         TEXT NOT NULL,   -- git SHA (docs-as-code) | semver (admin UI)
  status          TEXT NOT NULL DEFAULT 'DRAFT',
  content_type    TEXT NOT NULL DEFAULT 'markdown',  -- markdown | url | file
  content_url     TEXT,            -- khi content_type = url/file (object store)
  -- Chunking metadata (giữ ở version/job, không lặp từng chunk)
  chunker_version TEXT,
  chunk_size      INTEGER,
  chunk_overlap   INTEGER,
  content_hash    TEXT,            -- sha256(toàn bộ content)
  status_history  JSONB DEFAULT '[]',  -- CHỈ audit, không phải nguồn sự thật
  created_by      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source_id, version)
);
```

**State machine:**

```text
DRAFT
  ↓  review {decision: "approve"}
APPROVED
  ↓  publish (→ tạo ingestion job → INDEXING)
INDEXING
  ├──────────────┐
  ▼              ▼
PUBLISHED       FAILED
```

- `review approve`: `DRAFT → APPROVED` (không tạo job)
- `publish`: tạo `ai_ingestion_jobs` → `INDEXING`; **chỉ hợp lệ khi status = APPROVED** (nếu không → 409)
- Worker xong → **atomic transaction** (không bao giờ để `job = completed` mà
  `version = INDEXING`, và không bao giờ `version = PUBLISHED` với
  `active_version_id` cũ):

  ```sql
  BEGIN;
  UPDATE ai_ingestion_jobs
     SET status = 'completed'
   WHERE id = $job_id;
  UPDATE ai_knowledge_source_versions
     SET status = 'PUBLISHED'
   WHERE id = $version_id AND source_id = $source_id;  -- phải khớp source
  UPDATE ai_knowledge_sources
     SET active_version_id = $version_id, updated_at = now()
   WHERE id = $source_id;
  COMMIT;
  ```

  Rowcount mismatch (`UPDATE ... RETURNING` không trả đúng 1 row) → ROLLBACK →
  job FAILED.
- Version cũ vẫn phục vụ query cho đến khi version mới PUBLISHED (không downtime)
- Version FAILED **không** làm mất version cũ đang phục vụ

### 3.3 `ai_knowledge_chunks` — gắn source_version, idempotent

```sql
CREATE TABLE IF NOT EXISTS public.ai_knowledge_chunks (
  id              BIGSERIAL PRIMARY KEY,
  source_version_id BIGINT NOT NULL REFERENCES ai_knowledge_source_versions(id) ON DELETE CASCADE,
  chunk_index     INTEGER NOT NULL,
  heading         TEXT,
  content         TEXT NOT NULL,
  -- Idempotency: sha256(source_version_id + chunk_index + content_hash + chunker_version)
  chunk_id        TEXT NOT NULL UNIQUE,
  content_hash    TEXT NOT NULL,
  embedding       vector(1024),
  embedding_model TEXT,
  embedding_dimensions INTEGER,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_chunks_source_version
  ON ai_knowledge_chunks(source_version_id);
CREATE INDEX IF NOT EXISTS idx_chunks_chunk_id
  ON ai_knowledge_chunks(chunk_id);
CREATE INDEX IF NOT EXISTS idx_chunks_embedding_hnsw
  ON ai_knowledge_chunks USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);
```

- `chunk_id` unique → `INSERT ... ON CONFLICT (chunk_id) DO NOTHING` → worker
  chạy lại không tạo duplicate (idempotent)
- Query chỉ đọc chunks thuộc `active_version_id` của source

### 3.4 `ai_ingestion_jobs` — Postgres durable queue

```sql
CREATE TABLE IF NOT EXISTS public.ai_ingestion_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_version_id BIGINT NOT NULL REFERENCES ai_knowledge_source_versions(id) ON DELETE CASCADE,
  status          TEXT NOT NULL DEFAULT 'pending',  -- pending | processing | completed | failed
  locked_by       TEXT,          -- worker ID
  locked_at       TIMESTAMPTZ,   -- lease (heartbeat cập nhật)
  attempts        INTEGER DEFAULT 0,
  max_attempts    INTEGER DEFAULT 3,
  error_message   TEXT,
  total_chunks    INTEGER DEFAULT 0,
  embedded_chunks INTEGER DEFAULT 0,
  next_retry_at   TIMESTAMPTZ,      -- backoff: claim lại chỉ sau mốc này
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_claim
  ON ai_ingestion_jobs(status, created_at)
  WHERE status = 'pending';
```

### 3.5 `ai_rag_runs` — trace mỗi query (base cho eval)

```sql
CREATE TABLE IF NOT EXISTS public.ai_rag_runs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT,
  query           TEXT NOT NULL,
  rewritten_query TEXT,
  retrieved_count INTEGER,
  reranked_count  INTEGER,
  hit_ids         TEXT[],          -- source_version_id/chunk refs
  latency_ms      INTEGER,
  model_used      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.6 `ai_rag_feedback` — gắn run_id

```sql
CREATE TABLE IF NOT EXISTS public.ai_rag_feedback (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id          UUID NOT NULL REFERENCES ai_rag_runs(id),
  helpful         BOOLEAN NOT NULL,
  comment         TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.7 `ai_rag_eval` — eval dataset

```sql
CREATE TABLE IF NOT EXISTS public.ai_rag_eval (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  query           TEXT NOT NULL,
  expected_answer TEXT NOT NULL,
  tenant_id       TEXT,
  tags            TEXT[] DEFAULT '{}',
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.8 Migrations — coordination

Migration chạy lúc startup (pattern ai-service hiện có: `migration.Run` trước
khi serve). **`rag-api` 2 replicas → phải có single-writer / advisory lock**
(Postgres `pg_advisory_lock`) hoặc migration framework có locking — không để 2
instance chạy migration đồng thời không coordination. (`rag-worker` không chạy
migration.)

## 4. Ingestion pipeline

### 4.1 Worker architecture

```text
Deployment: rag-api        (container: rag-api,    replicas: 2, uvicorn app.main:app)
Deployment: rag-worker     (container: rag-worker, replicas: 2, python -m app.worker)
```

**Cùng image `arda/rag-service:<version>`, hai Deployment riêng** (API scale
theo query traffic, worker scale theo ingestion; API restart không ảnh hưởng
worker và ngược lại; resource/HPA độc lập).

### 4.2 Vòng đời job (Postgres queue — SKIP LOCKED)

```text
1. CLAIM:
   UPDATE ai_ingestion_jobs
   SET status = 'processing', locked_by = $worker, locked_at = now(),
       attempts = attempts + 1
   WHERE id = (SELECT id FROM ai_ingestion_jobs
               WHERE status = 'pending' AND attempts < max_attempts
                 AND (next_retry_at IS NULL OR next_retry_at <= now())
               ORDER BY created_at ASC LIMIT 1
               FOR UPDATE SKIP LOCKED)
   RETURNING id;

2. CHUNK:   LlamaIndex NodeParser → chunk_id idempotent
            → INSERT ... ON CONFLICT (chunk_id) DO NOTHING

3. EMBED:   batch 16 texts/call, 4 concurrent, exponential backoff
            (500ms → 2s → 8s), tối đa 3 retry
            → VALIDATE dimension: configured != actual → FAILED (không lưu)
            → **retry resume (idempotent):** chunk đã có `embedding IS NOT NULL`
              → skip embed; chunk chỉ có row text mà `embedding IS NULL` → embed.
              Chunk tồn tại nhờ `chunk_id` unique từ lần chạy trước, nên chạy
              lại không nhân đôi row, chỉ bù vector còn thiếu.

4. COMPLETE: status = 'completed'
            → transaction: version.status = 'PUBLISHED',
              source.active_version_id = version.id (xem §3.2 — atomic,
              rowcount-checked)

5. RETRY:   embedding/chunking lỗi (attempts < max_attempts)
            → status = 'pending' (requeue), lease release
            → `next_retry_at = now() + backoff` (500ms → 2s → 8s theo attempts),
              claim lại sau mốc đó (xem claim predicate §4.2)

6. FAILED:  attempts >= max_attempts → status = 'failed', error_message
            → version.status = 'FAILED' (version cũ vẫn phục vụ)
```

### 4.3 Lease & heartbeat

- `locked_at` là **lease**, không phải trạng thái vĩnh viễn
- Worker xử lý job dài → heartbeat cập nhật `locked_at` mỗi 60s
- Job bị claim nhưng worker chết → lease hết hạn (`locked_at < now() -
  lease_duration`) → reset `pending`, worker khác claim lại (idempotency đảm
  bảo an toàn)

```yaml
worker:
  lease_duration: 5m
  heartbeat_interval: 60s
```

### 4.4 Embedding dimension validation

```python
expected = config.embedding_dimensions  # hoặc từ source version config
actual = len(vectors[0])
if actual != expected:
    job.fail(f"Dimension mismatch: expected {expected}, got {actual}")
    return  # Không lưu — reject ở job level, không lọt xuống pgvector
```

### 4.5 Embedding config — immutable contract Phase 1

```yaml
embedding:
  provider: openai-compatible   # LlamaIndex OpenAIEmbedding → Cloudflare Workers AI
  model: <CLOUDFLARE_WORKERS_AI_MODEL>   # vd: @cf/qwen/qwen3-embedding-0.6b (placeholder)
  dimensions: 1024              # PHẢI khớp vector(1024) trong schema
  api_key_env: CF_WORKERS_AI_API_TOKEN
  base_url: <WORKERS_AI_EMBEDDINGS_URL>
  batch_size: 16
  concurrency: 4
```

**`vector(1024)` + model embedding là immutable contract của Phase 1** — đổi
model khác dimension hoặc đổi `dimensions` ⇒ cần migration + re-index toàn bộ
dữ liệu qua version mới (không nhẹ nhàng). Dimension mismatch → job FAILED ở
§4.4 chứ không âm thầm chấp nhận.

## 5. Query pipeline

### 5.1 Pipeline

```text
Original query
      │
      ├────────────────────────────────┐
      │                                │
      ▼                                ▼
Security Context               [Query Rewrite — OPTIONAL, không default]
(tenant + permission                 │
 SQL filter TRƯỚC retrieval)         ▼
      │                     [chỉ rewrite khi cần clarification]
      ▼                                │
  Hybrid Retrieval ◄───────────────────┘
      │
      ├── FTS (BM25 / PostgreSQL FTS) ── RRF fusion
      ├── pgvector (cosine) ────────────
      │
      ▼
  RRF top-8 candidates
      │
      ▼
  Reranker (abstraction, config-driven)
      │  provider: anthropic (Phase 1) | none | cross_encoder (future)
      ▼
  top-3
      │
      ▼
  Citations / Context
```

- **SecurityContext là bước ĐẦU TIÊN** — SQL filter (tenant/scope/permission)
  chạy trước retrieval. Invariant: không document nào ngoài quyền user lọt vào
  candidate, LLM reranker, hay LLM context.
- **Query rewrite KHÔNG default.** Query đi thẳng vào hybrid retrieval; rewrite
  (LLM, model rẻ) chỉ khi query có dấu hiệu cần clarification. Mặc định `off`.

### 5.2 Reranker config

```yaml
reranker:
  provider: anthropic          # none | anthropic | cross_encoder (future)
  model: claude-haiku-4-5-20251001
  api_key_env: ANTHROPIC_API_KEY
  timeout_ms: 1500
  candidates: 8
  top_n: 3
```

Abstraction (không phụ thuộc trực tiếp Anthropic SDK):

```python
class Reranker(Protocol):
    async def rerank(
        self,
        query: str,
        candidates: list[Candidate],
        top_n: int,
    ) -> list[Candidate]:
        ...
```

Sau này đổi BGE/Cohere/local chỉ thay implementation.

### 5.3 Retrieval metadata filter (trong SQL, trước ranking)

```sql
tenant_id = $1
AND scope IN ('tenant', 'global')     -- system không expose cho assistant
AND source.active_version_id = version.id
AND version.status = 'PUBLISHED'
AND (source.effective_from IS NULL OR source.effective_from <= now())
AND (source.effective_to   IS NULL OR source.effective_to   > now())  -- `>` chứ không `>=`
AND source.deleted_at IS NULL         -- soft-delete không phục vụ query
```

## 6. Security contract

### 6.1 Invariant bất biến

```text
UNTRUSTED REQUEST
      ↓
AUTHENTICATED SECURITY CONTEXT
      ↓
TENANT + PERMISSION SQL FILTER   ← TRƯỚC retrieval/rerank (data leakage guard)
      ↓
RETRIEVAL → FTS + Vector
      ↓
RRF fusion
      ↓
RERANK
      ↓
TOP-K
      ↓
LLM
```

### 6.2 Trusted delegated identity (reuse `arda-grpc/identity` contract)

Arda đã có cơ chế service-to-service auth đúng chuẩn (Go:
`libs/go/arda-grpc/identity`): HMAC-SHA256 signed token
`v1.{base64(claims)}.{base64(hmac)}` với claims `{v, src, aud, iat, exp, nonce}`,
TTL ≤ 5 phút, verify signature + audience + expiry. **rag-service reuse đúng
contract này, implement Python tương đương** (không import Go).

```text
ai-service / auth-gateway
      │  x-service-auth: signed identity {src, aud: rag-service}
      │  + X-Tenant-Id, X-User-Id, X-Permissions (gắn với identity đã xác thực)
      ▼
rag-service middleware
      │  1. Verify x-service-auth (HMAC + aud + exp + source ∈ trusted set)
      │  2. FAIL-CLOSED: thiếu/xấu token → 401
      │  3. CHỈ sau verify: build SecurityContext từ X-* headers
      ▼
SecurityContext → SQL filter → retrieval
```

**Contract:**

| Header | Được tin khi | Provenance |
|---|---|---|
| `x-service-auth` | Luôn verify (HMAC + aud + exp + source ∈ trusted set) | Điều kiện tiên quyết |
| `X-Tenant-Id`, `X-User-Id` | Sau khi verify service auth + source ∈ trusted | Gateway/ai-service injected |
| `X-Permissions` | Chỉ đọc khi source = `auth-gateway` (đã check policy) | Gateway policy |
| `X-Auth-Checked` | **Không bao giờ tin một mình** — chỉ signal sau verify | — |

**Rule:** `tenant_id`, `user_id`, `permissions` chỉ từ `SecurityContext`
(server-derived). Request body **không bao giờ** chứa identity. Query request
chỉ có `{query, top_k}`.

## 7. API chi tiết

### 7.1 POST /api/rag/query (ai-service → rag-service, nội bộ)

```jsonc
// Request
{ "query": "cách tính phép năm", "top_k": 3 }

// Response 200
{
  "run_id": "uuid",
  "hits": [
    {
      "source_id": "123",
      "source_version_id": "456",
      "version": "abc1234",
      "title": "Chính sách nhân sự",
      "heading": "Quy định nghỉ phép",
      "content": "Mỗi nhân viên được hưởng 12 ngày phép năm...",
      "score": 0.92,
      "citation": "[123:Quy định nghỉ phép]"
    }
  ],
  "latency_ms": 1200,
  "rewritten": false,
  "retrieved_count": 8,
  "reranked_count": 3
}
```

**top_k bounds (server-side enforce):** `1 ≤ top_k ≤ 10`. Phase 1 config mặc định:

```yaml
retrieval:
  vector_top_k: 8          # Số candidate từ vector search
  fts_top_k: 8             # Số candidate từ FTS
  rrf_k: 60                # Hằng số k trong RRF(d) = Σ 1/(k + rank_i(d))
  rerank_candidates: 8     # Số candidate đưa vào reranker
  final_top_k: 3           # Số hit cuối cùng trả về (server clamp)
```

Không trả embedding, raw retrieval scores, hay permission metadata.

### 7.2 Sources & versions (version-centric — content nằm trong body tạo version)

```http
POST   /sources
GET    /sources                          # mặc định AND deleted_at IS NULL (không trả source đã xoá mềm)
DELETE /sources/{source_id}

POST   /sources/{source_id}/versions    # body: {version, content_type, content|content_url, chunker_config}
GET    /sources/{source_id}/versions

POST   /sources/{source_id}/versions/{version_id}/review   # {decision: approve|reject, reason}
POST   /sources/{source_id}/versions/{version_id}/publish
```

Chunker config (ở version, không lặp từng chunk):

```jsonc
{
  "version": "1.0.0",
  "content_type": "markdown",
  "content": "...",
  "chunker_config": {
    "strategy": "heading",
    "chunk_size": 512,
    "chunk_overlap": 64,
    "chunker_version": "1"
  }
}
```

### 7.3 Jobs / Feedback

```http
GET  /jobs/{job_id}
POST /feedback   # {run_id, helpful, comment}
```

## 8. Evaluation

### 8.1 Eval set

Bộ câu hỏi mẫu + expected answer (`ai_rag_eval`), theo domain (hrm policy,
crm, finance, system usage FAQ).

### 8.2 RAGAS metrics (script offline — không chặn request)

| Metric | Đo gì |
|---|---|
| Faithfulness | Answer được support bởi retrieved context không |
| Context Precision | Bao nhiêu retrieved chunks thực sự liên quan |
| Context Recall | Bao nhiêu chunks liên quan được retrieve ra |
| Answer Relevancy | Answer trả lời đúng câu hỏi |

### 8.3 Vòng lặp & gate

`eval set → pipeline → RAGAS score → cải tiến config → đo lại`

**Ngưỡng initial gate (placeholder):** faithfulness ≥ 0.85, context recall ≥
0.7. **KHÔNG hard-code** — calibrate sau khi có baseline thực từ eval set.

Gate P4 (switch production) chỉ sau khi score đạt ngưỡng calibrate.

## 9. Rollout

| Phase | Nội dung | Kết quả |
|---|---|---|
| P1 | Scaffold + schema + migrations + FastAPI skeleton + security middleware | Service chạy, auth verified |
| P2 | Ingestion (chunker + embedder + queue + worker) | Index được source qua admin API |
| P3 | Query pipeline (retrieval + RRF + rerank) | API trả hits đúng scope |
| P4 | ai-service integration (svcclient → rag-service) + gateway policy | Chat dùng RAG thay vì SQL search |
| P5 | Admin UI (mfe) + eval harness + feedback loop | Người dùng tự quản lý kho kiến thức |

## 10. Observability & Data retention

- **Metrics:** query latency (p50/p95), retrieval/rerank/embed latency, job
  success/failure rate, queue depth, retry count
- **Logs:** structured JSON, mọi log trong query lifecycle gắn `run_id`
- **`ai_rag_runs`:** query, rewritten query, retrieved/reranked counts, hit
  ids, latency, model → base cho eval

**PII/data retention:** `ai_rag_runs.query` có thể chứa dữ liệu người dùng
nhạy cảm → **retention policy** (ví dụ: xoá query cũ sau N ngày, hoặc chỉ giữ
query đã redact), **tránh log duplicate full query** ở nơi không cần. Chi tiết
retention window chốt khi implement với data-governance.

## 11. Deployment (GitOps)

- 2 Deployment: `rag-api` (2 replicas, HPA sau), `rag-worker` (2 replicas,
  cùng image khác command)
- Manifest mới `arda-infra/k8s/apps/rag-service/`; image đẩy GHCR qua
  `images.yml`
- Migration startup với advisory lock (§3.8) — không dùng job riêng
- `policy.yaml` thêm routes: `/api/rag/query` (service-to-service),
  `/api/rag/sources*` (admin, `ai.knowledge.manage`)

## 12. Folder structure

```text
arda-be/apps/rag-service/
├── app/
│   ├── main.py              # FastAPI app, lifespan, wiring, advisory-lock migration
│   ├── config.py            # Pydantic Settings (env → config)
│   ├── api/                 # routes (query, sources, versions, jobs, feedback)
│   ├── domain/              # models (pydantic), security.py, errors.py
│   ├── service/             # ingestion, query_pipeline, feedback
│   ├── rag/                 # retriever (FTS+vector+RRF), reranker, chunker, embedder
│   ├── db/                  # engine, tables, queue (SKIP LOCKED)
│   └── worker.py            # CLI: ingestion worker loop
├── migrations/
│   ├── 001_rag_foundation.sql
│   └── 002_rag_eval.sql
├── tests/
├── Dockerfile
├── pyproject.toml
└── README.md
```

## 13. Dependencies

```toml
[tool.poetry.dependencies]
python = "^3.13"
fastapi = "^0.141"
uvicorn = { version = "^0.52", extras = ["standard"] }
sqlalchemy = "^2.0.52"
psycopg = { version = "^3.3", extras = ["binary"] }
pydantic-settings = "^2.15"
# LOCK EXACT khi implement — ecosystem LlamaIndex thay đổi nhanh
llama-index-core = "0.14.24"
llama-index-vector-stores-postgres = "0.9.0"
llama-index-embeddings-openai = "0.7.0"
anthropic = "^1.3"
```

Bỏ `httpx` — LlamaIndex embedder tự xử lý OpenAI-compatible HTTP client.
