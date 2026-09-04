-- +goose Up
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

-- +goose Down
DROP TABLE IF EXISTS public.ai_tenant_rag_strategies;
DROP TABLE IF EXISTS public.ai_tenant_guardrails;
