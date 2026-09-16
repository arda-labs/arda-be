-- +goose Up
-- HITL execution must run the exact JSON the approver saw and approved.
-- `arguments_redacted` is display/audit only: it is sanitized (bearer /
-- authorization / arda_sid patterns) and bounded to 16 KiB, so feeding it back
-- to a tool can execute a different action than the one approved. The original
-- payload is persisted here (encrypted by the service when an encryption
-- secret is configured) and only the resume path reads it; the redacted copy
-- keeps its display/audit meaning.
ALTER TABLE public.ai_tool_executions
    ADD COLUMN IF NOT EXISTS arguments_encrypted TEXT;

-- Existing proposals have no original copy. The redacted payload is the best
-- available evidence; it stays as-is and the resume path still refuses to
-- execute anything that does not parse as bounded JSON.
UPDATE public.ai_tool_executions
SET arguments_encrypted = arguments_redacted::text
WHERE arguments_encrypted IS NULL;

-- +goose Down
ALTER TABLE public.ai_tool_executions
    DROP COLUMN IF EXISTS arguments_encrypted;
