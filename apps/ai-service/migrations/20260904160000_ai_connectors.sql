-- +goose Up
CREATE TABLE IF NOT EXISTS public.ai_knowledge_connectors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    target_source VARCHAR(256) NOT NULL,
    sync_schedule VARCHAR(64) NOT NULL DEFAULT 'Hourly',
    status VARCHAR(32) NOT NULL DEFAULT 'synced',
    last_sync_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    doc_count INT NOT NULL DEFAULT 0,
    total_chunks INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ai_knowledge_connectors_tenant_idx
    ON public.ai_knowledge_connectors (tenant_id);

-- +goose Down
DROP TABLE IF EXISTS public.ai_knowledge_connectors;
