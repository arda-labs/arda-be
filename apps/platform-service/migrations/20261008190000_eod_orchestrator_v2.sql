-- +goose Up
ALTER TABLE plt_job_definitions
    ADD COLUMN IF NOT EXISTS module VARCHAR(32) NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS depends_on TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS mandatory BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS stop_on_fail BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS retryable BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE plt_job_runs
    ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 0;

CREATE TABLE plt_eod_runs (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    eod_date DATE NOT NULL UNIQUE,
    scope_type VARCHAR(16) NOT NULL CHECK (scope_type = 'SYSTEM'),
    status VARCHAR(16) NOT NULL CHECK (status IN ('RUNNING', 'SUCCEEDED', 'FAILED')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    error TEXT
);

-- +goose Down
DROP TABLE IF EXISTS plt_eod_runs;
ALTER TABLE plt_job_definitions
    DROP COLUMN IF EXISTS retryable,
    DROP COLUMN IF EXISTS stop_on_fail,
    DROP COLUMN IF EXISTS mandatory,
    DROP COLUMN IF EXISTS depends_on,
    DROP COLUMN IF EXISTS module;
ALTER TABLE plt_job_runs DROP COLUMN IF EXISTS attempt;
