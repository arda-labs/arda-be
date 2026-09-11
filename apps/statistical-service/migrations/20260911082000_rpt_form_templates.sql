-- +goose Up

-- QCMS form templates (W5b): Excel/schema template catalog with JSON
-- export/import. media_file_id references the media-service file (optional).

CREATE TABLE IF NOT EXISTS rpt_form_templates (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         VARCHAR(64) NOT NULL,
    code              VARCHAR(64) NOT NULL,
    name              VARCHAR(255) NOT NULL,
    media_file_id     UUID,
    schema            JSONB NOT NULL DEFAULT '{}',
    workflow_case_type VARCHAR(64) NOT NULL DEFAULT '',
    is_active         BOOLEAN NOT NULL DEFAULT true,
    created_by        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

-- +goose Down

DROP TABLE IF EXISTS rpt_form_templates;
