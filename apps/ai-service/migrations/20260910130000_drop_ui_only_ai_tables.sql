-- +goose Up
-- Removes AI config surfaces that were exposed in the UI but never enforced
-- on the run/retrieval path (agents, guardrails, RAG strategies, connectors),
-- plus the hardcoded-price cost ledger column.
DROP TABLE IF EXISTS public.ai_knowledge_connectors;
ALTER TABLE public.ai_runs DROP COLUMN IF EXISTS cost_usd;
DROP TABLE IF EXISTS public.ai_tenant_rag_strategies;
DROP TABLE IF EXISTS public.ai_tenant_guardrails;
DROP TABLE IF EXISTS public.ai_agents;

-- +goose Down
CREATE TABLE IF NOT EXISTS public.ai_agents (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    department VARCHAR(64) NOT NULL,
    description TEXT,
    system_prompt TEXT NOT NULL,
    model_id VARCHAR(128) NOT NULL,
    temperature REAL NOT NULL DEFAULT 0.2,
    allowed_tools TEXT[] NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_agents_tenant_name_uq UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS public.ai_tenant_guardrails (
    tenant_id VARCHAR(64) PRIMARY KEY,
    prompt_injection_defense BOOLEAN NOT NULL DEFAULT true,
    pii_masking BOOLEAN NOT NULL DEFAULT true,
    hallucination_check BOOLEAN NOT NULL DEFAULT true,
    zero_retention BOOLEAN NOT NULL DEFAULT true,
    injection_threshold REAL NOT NULL DEFAULT 0.85,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.ai_tenant_rag_strategies (
    tenant_id VARCHAR(64) PRIMARY KEY,
    strategy VARCHAR(64) NOT NULL DEFAULT 'hierarchical',
    parent_chunk_size INT NOT NULL DEFAULT 1024,
    child_chunk_size INT NOT NULL DEFAULT 256,
    similarity_threshold REAL NOT NULL DEFAULT 0.82,
    reranker_model VARCHAR(128) NOT NULL DEFAULT 'cohere-rerank-v3.5',
    top_k INT NOT NULL DEFAULT 20,
    top_n INT NOT NULL DEFAULT 5,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

ALTER TABLE public.ai_runs ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(18, 8) NOT NULL DEFAULT 0;
