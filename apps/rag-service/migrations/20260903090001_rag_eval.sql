-- +goose Up
CREATE TABLE IF NOT EXISTS public.ai_rag_eval (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  query           TEXT NOT NULL,
  expected_answer TEXT NOT NULL,
  tenant_id       TEXT,
  tags            TEXT[] DEFAULT '{}',
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS public.ai_rag_eval CASCADE;
