-- +goose Up
-- PostgreSQL 18 is the current Arda database target. No vector extension is
-- enabled here; embedding model/dimension/index approval is a separate gate.
CREATE SCHEMA IF NOT EXISTS ai;

CREATE TABLE IF NOT EXISTS ai.conversations (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    actor_user_id UUID NOT NULL,
    external_thread_id TEXT NOT NULL,
    title TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    summary TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_message_at TIMESTAMPTZ,
    CONSTRAINT ai_conversations_status_ck CHECK (status IN ('ACTIVE', 'ARCHIVED', 'DELETED')),
    CONSTRAINT ai_conversations_thread_key_uq UNIQUE (tenant_id, actor_user_id, external_thread_id)
);

CREATE TABLE IF NOT EXISTS ai.runs (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    conversation_id UUID NOT NULL REFERENCES ai.conversations(id),
    tenant_id VARCHAR(64) NOT NULL,
    actor_user_id UUID NOT NULL,
    external_run_id TEXT NOT NULL,
    status VARCHAR(32) NOT NULL,
    agent_id VARCHAR(128) NOT NULL DEFAULT 'arda-assistant',
    protocol_version VARCHAR(32) NOT NULL DEFAULT 'ag-ui-v1',
    provider VARCHAR(128),
    model_id VARCHAR(128),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    last_event_sequence BIGINT NOT NULL DEFAULT 0,
    usage JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code VARCHAR(128),
    idempotency_key VARCHAR(255),
    CONSTRAINT ai_runs_status_ck CHECK (status IN ('RUNNING', 'WAITING_APPROVAL', 'SUCCEEDED', 'FAILED', 'CANCELLED')),
    CONSTRAINT ai_runs_external_id_uq UNIQUE (tenant_id, external_run_id)
);

CREATE TABLE IF NOT EXISTS ai.messages (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    conversation_id UUID NOT NULL REFERENCES ai.conversations(id),
    run_id UUID REFERENCES ai.runs(id),
    sequence BIGINT NOT NULL,
    role VARCHAR(32) NOT NULL,
    content_type VARCHAR(64) NOT NULL DEFAULT 'text/plain',
    content TEXT NOT NULL,
    content_json JSONB,
    model_id VARCHAR(128),
    prompt_version VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_messages_role_ck CHECK (role IN ('user', 'assistant', 'tool', 'system')),
    CONSTRAINT ai_messages_sequence_uq UNIQUE (conversation_id, sequence)
);

CREATE TABLE IF NOT EXISTS ai.tool_executions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    run_id UUID NOT NULL REFERENCES ai.runs(id),
    tenant_id VARCHAR(64) NOT NULL,
    actor_user_id UUID NOT NULL,
    tool_name VARCHAR(255) NOT NULL,
    tool_version VARCHAR(64) NOT NULL,
    risk VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    arguments_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_redacted JSONB,
    policy_decision VARCHAR(64) NOT NULL,
    approval_id UUID,
    idempotency_key VARCHAR(255),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_code VARCHAR(128),
    CONSTRAINT ai_tool_executions_status_ck CHECK (status IN ('REQUESTED', 'DENIED', 'WAITING_APPROVAL', 'RUNNING', 'SUCCEEDED', 'FAILED'))
);

CREATE TABLE IF NOT EXISTS ai.approvals (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    run_id UUID NOT NULL REFERENCES ai.runs(id),
    tool_execution_id UUID REFERENCES ai.tool_executions(id),
    tenant_id VARCHAR(64) NOT NULL,
    requester_user_id UUID NOT NULL,
    approver_user_id UUID,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    summary_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    resource_version VARCHAR(255),
    permission_version VARCHAR(255),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_approvals_status_ck CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED', 'CONSUMED'))
);

CREATE TABLE IF NOT EXISTS ai.knowledge_sources (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64),
    scope VARCHAR(32) NOT NULL,
    source_type VARCHAR(64) NOT NULL,
    source_key VARCHAR(512) NOT NULL,
    title TEXT NOT NULL,
    owner VARCHAR(255),
    classification VARCHAR(64) NOT NULL DEFAULT 'internal',
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    version VARCHAR(128) NOT NULL,
    checksum VARCHAR(128) NOT NULL,
    effective_from TIMESTAMPTZ,
    effective_to TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_knowledge_sources_scope_ck CHECK (scope IN ('tenant', 'global', 'system')),
    CONSTRAINT ai_knowledge_sources_status_ck CHECK (status IN ('DRAFT', 'PUBLISHED', 'EXPIRED', 'DISABLED')),
    CONSTRAINT ai_knowledge_sources_version_uq UNIQUE (tenant_id, source_key, version)
);

CREATE TABLE IF NOT EXISTS ai.knowledge_chunks (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    source_id UUID NOT NULL REFERENCES ai.knowledge_sources(id),
    tenant_id VARCHAR(64),
    chunk_index INTEGER NOT NULL,
    heading TEXT,
    content TEXT NOT NULL,
    content_checksum VARCHAR(128) NOT NULL,
    token_count INTEGER,
    acl_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    embedding_model VARCHAR(128),
    embedding_dimensions INTEGER,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_knowledge_chunks_index_uq UNIQUE (source_id, chunk_index)
);

CREATE TABLE IF NOT EXISTS ai.feedback (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    actor_user_id UUID NOT NULL,
    conversation_id UUID NOT NULL REFERENCES ai.conversations(id),
    message_id UUID REFERENCES ai.messages(id),
    rating SMALLINT NOT NULL,
    reason VARCHAR(128),
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_feedback_rating_ck CHECK (rating BETWEEN 1 AND 5)
);

CREATE INDEX IF NOT EXISTS ai_conversations_owner_updated_idx
    ON ai.conversations (tenant_id, actor_user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS ai_runs_tenant_started_idx
    ON ai.runs (tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS ai_runs_conversation_started_idx
    ON ai.runs (conversation_id, started_at DESC);
CREATE INDEX IF NOT EXISTS ai_messages_conversation_sequence_idx
    ON ai.messages (conversation_id, sequence);
CREATE INDEX IF NOT EXISTS ai_tool_executions_tenant_status_idx
    ON ai.tool_executions (tenant_id, status, started_at DESC);
CREATE INDEX IF NOT EXISTS ai_approvals_tenant_status_idx
    ON ai.approvals (tenant_id, status, expires_at);
CREATE INDEX IF NOT EXISTS ai_knowledge_sources_scope_status_idx
    ON ai.knowledge_sources (tenant_id, scope, status);
CREATE INDEX IF NOT EXISTS ai_knowledge_chunks_source_idx
    ON ai.knowledge_chunks (source_id, chunk_index);

-- +goose Down
DROP TABLE IF EXISTS ai.feedback;
DROP TABLE IF EXISTS ai.knowledge_chunks;
DROP TABLE IF EXISTS ai.knowledge_sources;
DROP TABLE IF EXISTS ai.approvals;
DROP TABLE IF EXISTS ai.tool_executions;
DROP TABLE IF EXISTS ai.messages;
DROP TABLE IF EXISTS ai.runs;
DROP TABLE IF EXISTS ai.conversations;
DROP SCHEMA IF EXISTS ai;
