-- +goose Up
CREATE SEQUENCE IF NOT EXISTS workflow_case_code_seq START 1;

-- +goose Down
DROP SEQUENCE IF EXISTS workflow_case_code_seq;
