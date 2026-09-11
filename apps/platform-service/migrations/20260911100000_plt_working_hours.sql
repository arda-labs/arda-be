-- +goose Up

-- Working hours (ca làm việc) — EPAS ComCfgSysWorkingHour parity (W6a).

CREATE TABLE IF NOT EXISTS plt_working_hours (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     VARCHAR(64) NOT NULL,
    org_code      VARCHAR(64) NOT NULL DEFAULT '',
    day_of_week   SMALLINT NOT NULL,           -- ISO 1=Mon .. 7=Sun
    start_time    TIME NOT NULL,
    end_time      TIME NOT NULL,
    break_minutes INTEGER NOT NULL DEFAULT 0,
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_by    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       INTEGER NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, org_code, day_of_week)
);

-- +goose Down

DROP TABLE IF EXISTS plt_working_hours;
