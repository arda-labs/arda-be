-- +goose Up
ALTER TABLE public.ai_knowledge_source_versions
  ADD COLUMN content TEXT;

-- +goose Down
ALTER TABLE public.ai_knowledge_source_versions
  DROP COLUMN content;