-- +goose Up

ALTER TABLE public.ai_tenant_quota_settings
    ADD COLUMN IF NOT EXISTS monthly_token_limit BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tokens_used BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS period_start DATE NOT NULL DEFAULT date_trunc('month', now())::date;

ALTER TABLE public.ai_runs
    ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(18, 8) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS public.ai_quota_reservations (
    tenant_id VARCHAR(64) NOT NULL,
    external_run_id VARCHAR(128) NOT NULL,
    period_start DATE NOT NULL,
    reserved_tokens BIGINT NOT NULL,
    actual_tokens BIGINT,
    status VARCHAR(16) NOT NULL DEFAULT 'RESERVED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, external_run_id)
);

CREATE INDEX IF NOT EXISTS ai_quota_reservations_period_idx
    ON public.ai_quota_reservations (tenant_id, period_start, status);

-- Keep queue claims and lease recovery index-backed as the worker scales past
-- a single replica. The partial predicate mirrors processNextJob exactly.
CREATE INDEX IF NOT EXISTS ai_ingestion_jobs_claim_idx
    ON public.ai_ingestion_jobs (status, next_retry_at, created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS ai_ingestion_jobs_lease_idx
    ON public.ai_ingestion_jobs (status, locked_at)
    WHERE status = 'running';

CREATE INDEX IF NOT EXISTS ai_knowledge_sources_effective_idx
    ON public.ai_knowledge_sources (tenant_id, scope, effective_from, effective_to)
    WHERE deleted_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ai_knowledge_chunks_embedding_dimensions_ck'
    ) THEN
        ALTER TABLE public.ai_knowledge_chunks
            ADD CONSTRAINT ai_knowledge_chunks_embedding_dimensions_ck
            CHECK (embedding IS NULL OR embedding_dimensions = 1024);
    END IF;
END $$;

-- +goose Down
DROP INDEX IF EXISTS public.ai_quota_reservations_period_idx;
DROP TABLE IF EXISTS public.ai_quota_reservations;
ALTER TABLE public.ai_tenant_quota_settings
    DROP COLUMN IF EXISTS monthly_token_limit,
    DROP COLUMN IF EXISTS tokens_used,
    DROP COLUMN IF EXISTS period_start;
ALTER TABLE public.ai_runs DROP COLUMN IF EXISTS cost_usd;
DROP INDEX IF EXISTS public.ai_ingestion_jobs_claim_idx;
DROP INDEX IF EXISTS public.ai_ingestion_jobs_lease_idx;
DROP INDEX IF EXISTS public.ai_knowledge_sources_effective_idx;
ALTER TABLE public.ai_knowledge_chunks
    DROP CONSTRAINT IF EXISTS ai_knowledge_chunks_embedding_dimensions_ck;
