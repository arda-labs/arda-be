-- +goose Up
CREATE TABLE plt_business_dates (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    scope_type VARCHAR(16) NOT NULL CHECK (scope_type IN ('SYSTEM', 'ORG')),
    org_code VARCHAR(64),
    prev_business_date DATE NOT NULL,
    business_date DATE NOT NULL,
    next_business_date DATE NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'EOD_PROCESSING', 'CLOSED')),
    last_eod_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((scope_type = 'SYSTEM' AND tenant_id IS NULL AND org_code IS NULL)
        OR (scope_type = 'ORG' AND tenant_id IS NOT NULL AND org_code IS NOT NULL AND org_code <> '')),
    UNIQUE NULLS NOT DISTINCT (tenant_id, scope_type, org_code)
);

CREATE TABLE plt_working_calendar_versions (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64),
    scope_type VARCHAR(16) NOT NULL CHECK (scope_type IN ('SYSTEM', 'ORG')),
    org_code VARCHAR(64),
    version INTEGER NOT NULL CHECK (version > 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((scope_type = 'SYSTEM' AND tenant_id IS NULL AND org_code IS NULL)
        OR (scope_type = 'ORG' AND tenant_id IS NOT NULL AND org_code IS NOT NULL AND org_code <> '')),
    UNIQUE NULLS NOT DISTINCT (tenant_id, scope_type, org_code, version)
);

CREATE UNIQUE INDEX ux_plt_working_calendar_active
    ON plt_working_calendar_versions (tenant_id, scope_type, org_code) NULLS NOT DISTINCT
    WHERE is_active;

CREATE TABLE plt_working_calendar_holidays (
    calendar_version_id VARCHAR(64) NOT NULL REFERENCES plt_working_calendar_versions(id),
    id VARCHAR(64) NOT NULL,
    holiday_date DATE NOT NULL,
    description VARCHAR(255) NOT NULL,
    is_recurring BOOLEAN NOT NULL DEFAULT FALSE,
    holiday_year INTEGER,
    PRIMARY KEY (calendar_version_id, holiday_date)
);

-- HEAD_OFFICE is explicitly mapped to global SYSTEM scope. Do not infer an
-- ORG mapping for any additional branch row: classify it before migration.
-- +goose StatementBegin
DO $$
DECLARE unmapped_branches TEXT;
DECLARE head_office_count INTEGER;
BEGIN
    SELECT count(*) INTO head_office_count FROM plt_system_dates WHERE branch_code = 'HEAD_OFFICE';
    IF head_office_count <> 1 THEN
        RAISE EXCEPTION 'expected exactly one HEAD_OFFICE business-date row, found %', head_office_count;
    END IF;
    SELECT string_agg(branch_code, ', ' ORDER BY branch_code)
      INTO unmapped_branches
      FROM plt_system_dates
     WHERE branch_code <> 'HEAD_OFFICE';
    IF unmapped_branches IS NOT NULL THEN
        RAISE EXCEPTION 'business-date scope mapping required for branches: %', unmapped_branches;
    END IF;
END $$;
-- +goose StatementEnd

INSERT INTO plt_business_dates (
    id, tenant_id, scope_type, org_code, prev_business_date,
    business_date, next_business_date, status, last_eod_at, updated_at
)
SELECT id, NULL, 'SYSTEM', NULL, previous_business_date,
       current_business_date, next_business_date, status, last_eod_at, updated_at
FROM plt_system_dates
WHERE branch_code = 'HEAD_OFFICE'
ON CONFLICT DO NOTHING;

INSERT INTO plt_working_calendar_versions (
    id, tenant_id, scope_type, org_code, version, is_active
)
VALUES ('calendar-system-v1', NULL, 'SYSTEM', NULL, 1, TRUE);

INSERT INTO plt_working_calendar_holidays (
    calendar_version_id, id, holiday_date, description, is_recurring, holiday_year
)
SELECT 'calendar-system-v1', id, holiday_date, description, is_recurring, holiday_year
FROM plt_holiday_calendars;

-- Keep old Platform pods compatible during the rolling release. Any remaining
-- legacy date writer mirrors its committed transition into the canonical row.
-- +goose StatementBegin
CREATE FUNCTION plt_sync_legacy_business_date() RETURNS trigger AS $$
BEGIN
    UPDATE plt_business_dates
       SET business_date = NEW.current_business_date,
           prev_business_date = NEW.previous_business_date,
           next_business_date = NEW.next_business_date,
           status = NEW.status,
           last_eod_at = NEW.last_eod_at,
           updated_at = NEW.updated_at
     WHERE id = NEW.id AND tenant_id IS NULL AND scope_type = 'SYSTEM' AND org_code IS NULL;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_plt_sync_legacy_business_date
AFTER UPDATE OF current_business_date, previous_business_date, next_business_date, status, last_eod_at
ON plt_system_dates FOR EACH ROW EXECUTE FUNCTION plt_sync_legacy_business_date();

-- +goose Down
DROP TRIGGER IF EXISTS trg_plt_sync_legacy_business_date ON plt_system_dates;
DROP FUNCTION IF EXISTS plt_sync_legacy_business_date();
DROP TABLE IF EXISTS plt_working_calendar_holidays;
DROP INDEX IF EXISTS ux_plt_working_calendar_active;
DROP TABLE IF EXISTS plt_working_calendar_versions;
DROP TABLE IF EXISTS plt_business_dates;
