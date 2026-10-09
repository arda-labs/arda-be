-- +goose Up
-- Retire the compatibility tables only after every reader and writer uses the
-- versioned business-date/calendar model. This guard also checks that no data
-- changed or was added to either representation during the transition.
-- +goose StatementBegin
DO $$
DECLARE
    legacy_date_count INTEGER;
    canonical_date_count INTEGER;
BEGIN
    IF to_regclass('public.plt_system_dates') IS NULL OR to_regclass('public.plt_holiday_calendars') IS NULL THEN
        RAISE EXCEPTION 'expected legacy business-date and holiday tables before replacement';
    END IF;

    SELECT count(*) INTO legacy_date_count FROM plt_system_dates;
    SELECT count(*) INTO canonical_date_count
      FROM plt_business_dates
     WHERE tenant_id IS NULL AND scope_type = 'SYSTEM' AND org_code IS NULL;
    IF legacy_date_count <> 1 OR canonical_date_count <> 1 THEN
        RAISE EXCEPTION 'expected one legacy HEAD_OFFICE row and one SYSTEM row; found legacy %, SYSTEM %', legacy_date_count, canonical_date_count;
    END IF;

    IF EXISTS (
        SELECT 1
          FROM plt_system_dates old
          FULL JOIN plt_business_dates current
            ON current.id = old.id
           AND current.tenant_id IS NULL
           AND current.scope_type = 'SYSTEM'
           AND current.org_code IS NULL
         WHERE old.branch_code IS DISTINCT FROM 'HEAD_OFFICE'
            OR current.id IS NULL
            OR old.id IS NULL
            OR old.current_business_date IS DISTINCT FROM current.business_date
            OR old.previous_business_date IS DISTINCT FROM current.prev_business_date
            OR old.next_business_date IS DISTINCT FROM current.next_business_date
            OR old.status IS DISTINCT FROM current.status
            OR old.last_eod_at IS DISTINCT FROM current.last_eod_at
    ) THEN
        RAISE EXCEPTION 'legacy HEAD_OFFICE and canonical SYSTEM business dates differ';
    END IF;

    IF EXISTS (
        (SELECT holiday_date, description, COALESCE(is_recurring, FALSE), holiday_year FROM plt_holiday_calendars
         EXCEPT
         SELECT holiday.holiday_date, holiday.description, holiday.is_recurring, holiday.holiday_year
           FROM plt_working_calendar_holidays holiday
           JOIN plt_working_calendar_versions version ON version.id = holiday.calendar_version_id
          WHERE version.tenant_id IS NULL AND version.scope_type = 'SYSTEM' AND version.org_code IS NULL AND version.is_active)
        UNION ALL
        (SELECT holiday.holiday_date, holiday.description, holiday.is_recurring, holiday.holiday_year
           FROM plt_working_calendar_holidays holiday
           JOIN plt_working_calendar_versions version ON version.id = holiday.calendar_version_id
          WHERE version.tenant_id IS NULL AND version.scope_type = 'SYSTEM' AND version.org_code IS NULL AND version.is_active
         EXCEPT
         SELECT holiday_date, description, COALESCE(is_recurring, FALSE), holiday_year FROM plt_holiday_calendars)
    ) THEN
        RAISE EXCEPTION 'legacy holidays and active SYSTEM calendar holidays differ';
    END IF;
END $$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_plt_sync_legacy_business_date ON plt_system_dates;
DROP FUNCTION IF EXISTS plt_sync_legacy_business_date();
DROP TABLE plt_holiday_calendars;
DROP TABLE plt_system_dates;

-- +goose Down
-- The prior schema cannot represent ORG scopes or calendar history. Refuse a
-- lossy rollback if those records have appeared since the forward migration.
-- +goose StatementBegin
DO $$
DECLARE
    system_date_count INTEGER;
    system_calendar_count INTEGER;
    active_system_calendar_count INTEGER;
BEGIN
    SELECT count(*) INTO system_date_count
      FROM plt_business_dates
     WHERE tenant_id IS NULL AND scope_type = 'SYSTEM' AND org_code IS NULL;
    SELECT count(*) INTO system_calendar_count
      FROM plt_working_calendar_versions
     WHERE tenant_id IS NULL AND scope_type = 'SYSTEM' AND org_code IS NULL;
    SELECT count(*) INTO active_system_calendar_count
      FROM plt_working_calendar_versions
     WHERE tenant_id IS NULL AND scope_type = 'SYSTEM' AND org_code IS NULL AND is_active;
    IF system_date_count <> 1 OR system_calendar_count <> 1 OR active_system_calendar_count <> 1
       OR EXISTS (SELECT 1 FROM plt_business_dates WHERE scope_type <> 'SYSTEM')
       OR EXISTS (SELECT 1 FROM plt_working_calendar_versions WHERE scope_type <> 'SYSTEM') THEN
        RAISE EXCEPTION 'cannot restore legacy date/calendar schema without losing ORG scopes or calendar history';
    END IF;
END $$;
-- +goose StatementEnd

CREATE TABLE plt_system_dates (
    id VARCHAR(64) PRIMARY KEY,
    branch_code VARCHAR(64) NOT NULL DEFAULT 'HEAD_OFFICE',
    current_business_date DATE NOT NULL,
    previous_business_date DATE NOT NULL,
    next_business_date DATE NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'OPEN',
    last_eod_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(branch_code)
);

INSERT INTO plt_system_dates (
    id, branch_code, current_business_date, previous_business_date,
    next_business_date, status, last_eod_at, updated_at
)
SELECT id, 'HEAD_OFFICE', business_date, prev_business_date,
       next_business_date, status, last_eod_at, updated_at
  FROM plt_business_dates
 WHERE tenant_id IS NULL AND scope_type = 'SYSTEM' AND org_code IS NULL;

CREATE TABLE plt_holiday_calendars (
    id VARCHAR(64) PRIMARY KEY,
    holiday_date DATE NOT NULL,
    description VARCHAR(255) NOT NULL,
    is_recurring BOOLEAN DEFAULT FALSE,
    holiday_year INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(holiday_date)
);

INSERT INTO plt_holiday_calendars (id, holiday_date, description, is_recurring, holiday_year)
SELECT holiday.id, holiday.holiday_date, holiday.description, holiday.is_recurring, holiday.holiday_year
  FROM plt_working_calendar_holidays holiday
  JOIN plt_working_calendar_versions version ON version.id = holiday.calendar_version_id
 WHERE version.tenant_id IS NULL AND version.scope_type = 'SYSTEM' AND version.org_code IS NULL AND version.is_active;

-- Restore the rolling-release compatibility path when this migration is
-- rolled back, so an older Platform pod can still update the canonical row.
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
