-- +goose Up

CREATE TABLE IF NOT EXISTS plt_system_dates (
    id VARCHAR(64) PRIMARY KEY,
    branch_code VARCHAR(64) NOT NULL DEFAULT 'HEAD_OFFICE',
    current_business_date DATE NOT NULL,
    previous_business_date DATE NOT NULL,
    next_business_date DATE NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'OPEN', -- OPEN, EOD_PROCESSING, CLOSED
    last_eod_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(branch_code)
);

CREATE TABLE IF NOT EXISTS plt_holiday_calendars (
    id VARCHAR(64) PRIMARY KEY,
    holiday_date DATE NOT NULL,
    description VARCHAR(255) NOT NULL,
    is_recurring BOOLEAN DEFAULT FALSE,
    holiday_year INT, -- Nullable, used when is_recurring = false
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(holiday_date)
);

CREATE TABLE IF NOT EXISTS plt_cutoff_configs (
    id VARCHAR(64) PRIMARY KEY,
    channel_code VARCHAR(64) NOT NULL,
    transaction_type VARCHAR(64) NOT NULL,
    cutoff_time TIME NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(channel_code, transaction_type)
);

-- Seed initial records
INSERT INTO plt_system_dates (id, current_business_date, previous_business_date, next_business_date)
VALUES ('sys_date_head_office', '2026-06-29', '2026-06-26', '2026-06-30')
ON CONFLICT DO NOTHING;

INSERT INTO plt_cutoff_configs (id, channel_code, transaction_type, cutoff_time) VALUES
    ('cutoff_citad_transfer', 'CITAD', 'TRANSFER', '16:30:00'),
    ('cutoff_napas_transfer', 'NAPAS', 'TRANSFER', '17:00:00'),
    ('cutoff_counter_deposit', 'COUNTER', 'DEPOSIT', '17:00:00')
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS plt_cutoff_configs CASCADE;
DROP TABLE IF EXISTS plt_holiday_calendars CASCADE;
DROP TABLE IF EXISTS plt_system_dates CASCADE;
