-- +goose Up

CREATE TABLE IF NOT EXISTS public.ai_knowledge_sources (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT,
    title TEXT NOT NULL,
    description TEXT,
    source_type TEXT NOT NULL DEFAULT 'docs',
    scope TEXT NOT NULL DEFAULT 'tenant',
    classification TEXT NOT NULL DEFAULT 'internal',
    language TEXT DEFAULT 'vi',
    tags TEXT[] DEFAULT '{}',
    owner_id TEXT,
    effective_from TIMESTAMPTZ,
    effective_to TIMESTAMPTZ,
    active_version_id BIGINT,
    deleted_at TIMESTAMPTZ,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.ai_knowledge_source_versions (
    id BIGSERIAL PRIMARY KEY,
    source_id BIGINT NOT NULL REFERENCES public.ai_knowledge_sources(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'DRAFT',
    content_type TEXT NOT NULL DEFAULT 'markdown',
    content TEXT,
    content_url TEXT,
    chunker_version TEXT DEFAULT '1',
    chunk_size INTEGER DEFAULT 512,
    chunk_overlap INTEGER DEFAULT 64,
    content_hash TEXT,
    status_history JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_source_version_unique UNIQUE (source_id, version)
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_ai_knowledge_sources_active_version'
    ) THEN
        ALTER TABLE public.ai_knowledge_sources
            ADD CONSTRAINT fk_ai_knowledge_sources_active_version
            FOREIGN KEY (active_version_id) REFERENCES public.ai_knowledge_source_versions(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS public.ai_knowledge_chunks (
    id BIGSERIAL PRIMARY KEY,
    source_version_id BIGINT NOT NULL REFERENCES public.ai_knowledge_source_versions(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    heading TEXT,
    content TEXT NOT NULL,
    chunk_id TEXT UNIQUE NOT NULL,
    content_hash TEXT NOT NULL,
    embedding vector(1024),
    embedding_model TEXT,
    embedding_dimensions INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ai_knowledge_chunks_embedding_hnsw_idx
    ON public.ai_knowledge_chunks
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

CREATE TABLE IF NOT EXISTS public.ai_ingestion_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_version_id BIGINT NOT NULL REFERENCES public.ai_knowledge_source_versions(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    locked_by TEXT,
    locked_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    error_message TEXT,
    total_chunks INTEGER NOT NULL DEFAULT 0,
    embedded_chunks INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.ai_rag_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT,
    query TEXT NOT NULL,
    retrieved_count INTEGER NOT NULL DEFAULT 0,
    reranked_count INTEGER NOT NULL DEFAULT 0,
    hit_ids BIGINT[] NOT NULL DEFAULT '{}',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    model_used TEXT NOT NULL DEFAULT 'fts-only',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.ai_rag_feedback (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL,
    helpful BOOLEAN NOT NULL,
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS public.ai_rag_feedback;
DROP TABLE IF EXISTS public.ai_rag_runs;
DROP TABLE IF EXISTS public.ai_ingestion_jobs;
DROP TABLE IF EXISTS public.ai_knowledge_chunks;
DROP TABLE IF EXISTS public.ai_knowledge_source_versions;
DROP TABLE IF EXISTS public.ai_knowledge_sources;
