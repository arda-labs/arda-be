-- +goose Up

-- P1b.4c: provision rate per debt group (evidence: EPAS CM130 catalog,
-- TT 02/2023: 0/5/20/50/100).

CREATE TABLE IF NOT EXISTS lnm_provision_rates (
    debt_group_code VARCHAR(32) PRIMARY KEY,
    rate_percent    NUMERIC(9,6) NOT NULL,
    effective_date  DATE NOT NULL DEFAULT '2026-01-01',
    updated_by      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO lnm_provision_rates (debt_group_code, rate_percent) VALUES
    ('GROUP_1', 0),  ('GROUP_2', 5),  ('GROUP_3', 20),
    ('GROUP_4', 50), ('GROUP_5', 100)
ON CONFLICT (debt_group_code) DO NOTHING;

CREATE TABLE IF NOT EXISTS lnm_provisions (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        VARCHAR(64) NOT NULL,
    agreement_code   VARCHAR(64) NOT NULL,
    debt_group_code  VARCHAR(32) NOT NULL,
    provision_date   DATE NOT NULL,
    outstanding_minor BIGINT NOT NULL,
    rate_percent     NUMERIC(9,6) NOT NULL,
    required_minor   BIGINT NOT NULL,
    delta_minor      BIGINT NOT NULL,
    journal_entry_id UUID,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, agreement_code, provision_date)
);

-- +goose Down
SELECT 1;
