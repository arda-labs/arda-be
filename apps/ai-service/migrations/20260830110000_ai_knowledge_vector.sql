-- +goose Up
-- Vector search for AI knowledge (roadmap §4.2): pgvector is already enabled
-- (20260826140000). The chunks table already carries embedding_model and
-- embedding_dimensions; comparisons are gated on matching model at query time
-- so switching providers never mixes vector spaces.
ALTER TABLE public.ai_knowledge_chunks
    ADD COLUMN IF NOT EXISTS embedding vector(1024);

CREATE INDEX IF NOT EXISTS ai_knowledge_chunks_embedding_hnsw_idx
    ON public.ai_knowledge_chunks
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- +goose Down
DROP INDEX IF EXISTS public.ai_knowledge_chunks_embedding_hnsw_idx;
ALTER TABLE public.ai_knowledge_chunks DROP COLUMN IF EXISTS embedding;
