-- +goose Up
CREATE TABLE workflow_process_definitions (
    id VARCHAR(64) PRIMARY KEY,
    process_code VARCHAR(100) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    bpmn_process_id VARCHAR(255) NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    resource_name VARCHAR(255) NOT NULL,
    xml_content TEXT NOT NULL,
    deployment_key BIGINT,
    status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',
    deployed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (version > 0),
    CHECK (status IN ('DRAFT', 'ACTIVE', 'INACTIVE'))
);

CREATE INDEX workflow_process_definitions_bpmn_process_id_idx
    ON workflow_process_definitions(bpmn_process_id);

-- +goose Down
DROP TABLE IF EXISTS workflow_process_definitions;
