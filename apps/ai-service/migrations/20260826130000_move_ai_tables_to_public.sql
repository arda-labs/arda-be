-- +goose Up
-- The AI database is already service-owned. Match the Arda convention of
-- keeping application tables in public while retaining an ai_ prefix.
ALTER TABLE ai.feedback SET SCHEMA public;
ALTER TABLE public.feedback RENAME TO ai_feedback;
ALTER TABLE ai.knowledge_chunks SET SCHEMA public;
ALTER TABLE public.knowledge_chunks RENAME TO ai_knowledge_chunks;
ALTER TABLE ai.knowledge_sources SET SCHEMA public;
ALTER TABLE public.knowledge_sources RENAME TO ai_knowledge_sources;
ALTER TABLE ai.approvals SET SCHEMA public;
ALTER TABLE public.approvals RENAME TO ai_approvals;
ALTER TABLE ai.tool_executions SET SCHEMA public;
ALTER TABLE public.tool_executions RENAME TO ai_tool_executions;
ALTER TABLE ai.messages SET SCHEMA public;
ALTER TABLE public.messages RENAME TO ai_messages;
ALTER TABLE ai.runs SET SCHEMA public;
ALTER TABLE public.runs RENAME TO ai_runs;
ALTER TABLE ai.conversations SET SCHEMA public;
ALTER TABLE public.conversations RENAME TO ai_conversations;
DROP SCHEMA ai;

-- +goose Down
CREATE SCHEMA ai;
ALTER TABLE public.ai_conversations RENAME TO conversations;
ALTER TABLE public.conversations SET SCHEMA ai;
ALTER TABLE public.ai_runs RENAME TO runs;
ALTER TABLE public.runs SET SCHEMA ai;
ALTER TABLE public.ai_messages RENAME TO messages;
ALTER TABLE public.messages SET SCHEMA ai;
ALTER TABLE public.ai_tool_executions RENAME TO tool_executions;
ALTER TABLE public.tool_executions SET SCHEMA ai;
ALTER TABLE public.ai_approvals RENAME TO approvals;
ALTER TABLE public.approvals SET SCHEMA ai;
ALTER TABLE public.ai_knowledge_sources RENAME TO knowledge_sources;
ALTER TABLE public.knowledge_sources SET SCHEMA ai;
ALTER TABLE public.ai_knowledge_chunks RENAME TO knowledge_chunks;
ALTER TABLE public.knowledge_chunks SET SCHEMA ai;
ALTER TABLE public.ai_feedback RENAME TO feedback;
ALTER TABLE public.feedback SET SCHEMA ai;
