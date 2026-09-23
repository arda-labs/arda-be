-- +goose Up
ALTER TABLE public.ai_conversations ADD COLUMN deleted_at TIMESTAMPTZ;
UPDATE public.ai_conversations SET deleted_at = updated_at WHERE status = 'DELETED';

CREATE INDEX ai_conversations_deleted_at_idx
    ON public.ai_conversations (deleted_at)
    WHERE status = 'DELETED';

CREATE TABLE public.ai_conversation_settings (
    tenant_id VARCHAR(64) PRIMARY KEY,
    trash_retention_months SMALLINT NOT NULL DEFAULT 1
        CHECK (trash_retention_months BETWEEN 1 AND 12),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE public.ai_conversation_settings;
DROP INDEX public.ai_conversations_deleted_at_idx;
ALTER TABLE public.ai_conversations DROP COLUMN deleted_at;
