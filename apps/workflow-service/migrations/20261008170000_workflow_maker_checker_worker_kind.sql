-- +goose Up

-- Shared maker-checker dispatch is opt-in per operation type. Keep this NULL
-- until a case type is explicitly canaried to common-maker-checker.
ALTER TABLE business_operation_types
    ADD COLUMN worker_kind TEXT;

-- +goose Down

-- Roll back only after all case types have cleared worker_kind and no runtime
-- depends on this column.
ALTER TABLE business_operation_types
    DROP COLUMN worker_kind;
