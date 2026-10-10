-- +goose Up

CREATE TABLE plt_scheduled_jobs (
    code                  VARCHAR(64) PRIMARY KEY CHECK (code ~ '^[a-z0-9-]+$'),
    name                  VARCHAR(255) NOT NULL,
    owner_service         VARCHAR(64)  NOT NULL,
    path                  TEXT NOT NULL CHECK (path LIKE '/internal/jobs/%'),
    cron_expr             VARCHAR(128),
    timezone              VARCHAR(64)  NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    enabled               BOOLEAN NOT NULL DEFAULT true,
    timeout_seconds       INTEGER NOT NULL CHECK (timeout_seconds BETWEEN 10 AND 3600),
    max_attempts          INTEGER NOT NULL CHECK (max_attempts BETWEEN 1 AND 5),
    retry_backoff_seconds INTEGER NOT NULL CHECK (retry_backoff_seconds BETWEEN 0 AND 3600),
    notify_tenant_id      VARCHAR(64) NOT NULL DEFAULT '',
    notify_user_ids       TEXT[] NOT NULL DEFAULT '{}',
    notify_group_ids      TEXT[] NOT NULL DEFAULT '{}',
    notify_role_codes     TEXT[] NOT NULL DEFAULT '{SUPER_ADMIN}',
    next_run_at           TIMESTAMPTZ,
    last_status           VARCHAR(16) CHECK (last_status IN ('SUCCEEDED','FAILED','TIMED_OUT')),
    retired_at            TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by            TEXT NOT NULL DEFAULT 'seed',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by            TEXT,
    version               INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_plt_scheduled_jobs_due ON plt_scheduled_jobs (next_run_at) WHERE enabled AND next_run_at IS NOT NULL;

CREATE TABLE plt_scheduled_job_runs (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    job_code       VARCHAR(64) NOT NULL REFERENCES plt_scheduled_jobs (code),
    trigger        VARCHAR(16) NOT NULL CHECK (trigger IN ('SCHEDULE','MANUAL')),
    scheduled_for  TIMESTAMPTZ NOT NULL,
    status         VARCHAR(16) NOT NULL CHECK (status IN ('RUNNING','SUCCEEDED','FAILED','TIMED_OUT','SKIPPED')),
    attempt        INTEGER NOT NULL DEFAULT 0,
    params         JSONB NOT NULL DEFAULT '{}',
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    deadline_at    TIMESTAMPTZ NOT NULL,
    error_code     VARCHAR(64),
    error_message  TEXT,
    result         JSONB,
    triggered_by   TEXT NOT NULL
);

CREATE INDEX idx_plt_scheduled_job_runs_job ON plt_scheduled_job_runs (job_code, scheduled_for DESC);
CREATE UNIQUE INDEX uq_plt_scheduled_job_runs_running ON plt_scheduled_job_runs (job_code) WHERE status = 'RUNNING';
CREATE INDEX idx_plt_scheduled_job_runs_deadline ON plt_scheduled_job_runs (deadline_at) WHERE status = 'RUNNING';

CREATE TABLE plt_outbox (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    subject          VARCHAR(160) NOT NULL,
    event_code       VARCHAR(120) NOT NULL,
    dedupe_key       VARCHAR(255) NOT NULL UNIQUE,
    payload          JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at     TIMESTAMPTZ,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT
);

CREATE INDEX idx_plt_outbox_pending ON plt_outbox (created_at, id) WHERE published_at IS NULL;

CREATE TABLE plt_audit_events (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   VARCHAR(64) NOT NULL,
    entity      VARCHAR(64) NOT NULL,
    entity_id   VARCHAR(128) NOT NULL,
    action      VARCHAR(32) NOT NULL,
    old_value   JSONB,
    new_value   JSONB,
    actor       TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_plt_audit_events_entity ON plt_audit_events (entity, entity_id, occurred_at DESC);

-- +goose Down
DROP TABLE IF EXISTS plt_audit_events;
DROP TABLE IF EXISTS plt_outbox;
DROP TABLE IF EXISTS plt_scheduled_job_runs;
DROP TABLE IF EXISTS plt_scheduled_jobs;
