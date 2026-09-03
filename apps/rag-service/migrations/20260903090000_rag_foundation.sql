-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
-- Rag-service owns the knowledge tables from this migration forward.
-- On production, the ai-service Go tables (ai_knowledge_sources,
-- ai_knowledge_chunks) already exist with a different shape. The migration
-- renames them to _v1 before creating the rag-shape tables.
-- ai-service must be scaled to 0 before this runs.

ALTER TABLE IF EXISTS public.ai_knowledge_sources RENAME TO ai_knowledge_sources_v1;
ALTER TABLE IF EXISTS public.ai_knowledge_chunks RENAME TO ai_knowledge_chunks_v1;

CREATE TABLE IF NOT EXISTS public.ai_knowledge_sources (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       TEXT,
  title           TEXT NOT NULL,
  description     TEXT,
  source_type     TEXT NOT NULL DEFAULT 'docs',
  scope           TEXT NOT NULL DEFAULT 'tenant',
  classification  TEXT NOT NULL DEFAULT 'internal',
  language        TEXT DEFAULT 'vi',
  tags            TEXT[] DEFAULT '{}',
  owner_id        TEXT,
  effective_from  TIMESTAMPTZ,
  effective_to    TIMESTAMPTZ,
  active_version_id BIGINT,          -- FK to ai_knowledge_source_versions, added after table creation
  deleted_at      TIMESTAMPTZ,
  created_by      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_source_versions (
  id              BIGSERIAL PRIMARY KEY,
  source_id       BIGINT NOT NULL REFERENCES ai_knowledge_sources(id) ON DELETE CASCADE,
  version         TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'DRAFT',
  content_type    TEXT NOT NULL DEFAULT 'markdown',
  content_url     TEXT,
  chunker_version TEXT,
  chunk_size      INTEGER,
  chunk_overlap   INTEGER,
  content_hash    TEXT,
  status_history  JSONB DEFAULT '[]',
  created_by      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source_id, version)
);

-- Now that ai_knowledge_source_versions exists, add the FK
ALTER TABLE public.ai_knowledge_sources
  ADD CONSTRAINT fk_active_version
  FOREIGN KEY (active_version_id) REFERENCES ai_knowledge_source_versions(id);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_chunks (
  id              BIGSERIAL PRIMARY KEY,
  source_version_id BIGINT NOT NULL REFERENCES ai_knowledge_source_versions(id) ON DELETE CASCADE,
  chunk_index     INTEGER NOT NULL,
  heading         TEXT,
  content         TEXT NOT NULL,
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

CREATE TABLE IF NOT EXISTS public.ai_ingestion_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_version_id BIGINT NOT NULL REFERENCES ai_knowledge_source_versions(id) ON DELETE CASCADE,
  status          TEXT NOT NULL DEFAULT 'pending',
  locked_by       TEXT,
  locked_at       TIMESTAMPTZ,
  attempts        INTEGER DEFAULT 0,
  max_attempts    INTEGER DEFAULT 3,
  error_message   TEXT,
  total_chunks    INTEGER DEFAULT 0,
  embedded_chunks INTEGER DEFAULT 0,
  next_retry_at   TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_claim
  ON ai_ingestion_jobs(status, created_at)
  WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS public.ai_rag_runs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT,
  query           TEXT NOT NULL,
  rewritten_query TEXT,
  retrieved_count INTEGER,
  reranked_count  INTEGER,
  hit_ids         TEXT[],
  latency_ms      INTEGER,
  model_used      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.ai_rag_feedback (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id          UUID NOT NULL REFERENCES ai_rag_runs(id),
  helpful         BOOLEAN NOT NULL,
  comment         TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS public.ai_rag_feedback CASCADE;
DROP TABLE IF EXISTS public.ai_rag_runs CASCADE;
DROP TABLE IF EXISTS public.ai_ingestion_jobs CASCADE;
DROP TABLE IF EXISTS public.ai_knowledge_chunks CASCADE;
DROP TABLE IF EXISTS public.ai_knowledge_source_versions CASCADE;
DROP TABLE IF EXISTS public.ai_knowledge_sources CASCADE;
ALTER TABLE IF EXISTS public.ai_knowledge_sources_v1 RENAME TO ai_knowledge_sources;
ALTER TABLE IF EXISTS public.ai_knowledge_chunks_v1 RENAME TO ai_knowledge_chunks;
