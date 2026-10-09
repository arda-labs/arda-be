-- +goose Up
-- Parameter and lookup ownership moved to platform-service. Runtime readers
-- now use the Platform RPC client; keep the legacy provision-rate source table
-- until the verified cross-database transfer has been run.
DROP TABLE IF EXISTS code_item;
DROP TABLE IF EXISTS code_set;
DROP TABLE IF EXISTS parameter;

-- +goose Down
-- The former local registry was transitional data. Recreate it through the
-- owning Platform service rather than restoring a second source of truth.
SELECT 1;
