-- +goose Up

-- Reusable email designs (W4+). A design holds one HTML layout that many event
-- templates can reference through noti_templates.design_code, so a brand/layout
-- change is made in one place. body_html on the template stays as an override
-- for one-off events.

CREATE TABLE IF NOT EXISTS noti_email_designs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  TEXT NOT NULL,
    code       TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    subject    TEXT NOT NULL DEFAULT '',
    body_html  TEXT NOT NULL DEFAULT '',
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, code)
);

ALTER TABLE noti_templates
    ADD COLUMN IF NOT EXISTS design_code TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE noti_templates
    DROP COLUMN IF EXISTS design_code;

DROP TABLE IF EXISTS noti_email_designs;
