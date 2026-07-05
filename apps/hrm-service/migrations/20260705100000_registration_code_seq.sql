-- +goose Up
CREATE SEQUENCE IF NOT EXISTS hrm_registration_code_seq START 1;

-- +goose Down
DROP SEQUENCE IF EXISTS hrm_registration_code_seq;
