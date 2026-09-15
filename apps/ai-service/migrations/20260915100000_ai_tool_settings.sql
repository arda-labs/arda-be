-- +goose Up

-- Platform-level AI tool governance (ADR-003). A row is an explicit runtime
-- override on top of the contract default compiled into generated.go. Row
-- absence means "follow the contract"; deleting the row restores the contract
-- default immediately. `enabled = false` is the operational kill switch;
-- `enabled = true` may only re-state a contract-enabled tool — the runtime can
-- never lift a contract `false`, because
-- effectiveEnabled = contractEnabled AND (overrideEnabled ?? true).
CREATE TABLE IF NOT EXISTS public.ai_tool_settings (
    method_name VARCHAR(128) PRIMARY KEY,
    enabled     BOOLEAN NOT NULL,
    updated_by  VARCHAR(64) NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS public.ai_tool_settings;
