-- +goose Up
CREATE TABLE process_mappings (
    business_key VARCHAR(255) PRIMARY KEY,
    process_instance_key BIGINT NOT NULL,
    bpmn_process_id VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS process_mappings;
