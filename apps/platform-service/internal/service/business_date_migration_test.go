package service

import (
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/apps/platform-service/migrations"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	"github.com/pressly/goose/v3"
)

func TestBusinessDateMigrationMapsHeadOfficeToSystem(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	var scope, businessDate, previousDate, nextDate string
	err := db.QueryRow("SELECT scope_type, business_date::text, prev_business_date::text, next_business_date::text "+
		"FROM plt_business_dates WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL").
		Scan(&scope, &businessDate, &previousDate, &nextDate)
	if err != nil {
		t.Fatalf("read migrated SYSTEM business date: %v", err)
	}
	if scope != "SYSTEM" || businessDate != "2026-06-29" || previousDate != "2026-06-26" || nextDate != "2026-06-30" {
		t.Fatalf("migrated date row = %s %s (%s, %s)", scope, businessDate, previousDate, nextDate)
	}
	var versions int
	if err := db.QueryRow("SELECT count(*) FROM plt_working_calendar_versions WHERE scope_type='SYSTEM' AND tenant_id IS NULL").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("system calendar versions = %d, want 1", versions)
	}
	var legacyDates, legacyCalendars sql.NullString
	if err := db.QueryRow("SELECT to_regclass('public.plt_system_dates')::text, to_regclass('public.plt_holiday_calendars')::text").Scan(&legacyDates, &legacyCalendars); err != nil {
		t.Fatal(err)
	}
	if legacyDates.Valid || legacyCalendars.Valid {
		t.Fatalf("legacy business-date tables still exist: dates=%v calendars=%v", legacyDates, legacyCalendars)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	// Roll back to the version just before the replacement migration so later
	// migrations do not shift which step this test undoes.
	const beforeLegacyReplacement = 20261008190000
	if err := goose.DownTo(db, ".", beforeLegacyReplacement, goose.WithAllowMissing()); err != nil {
		t.Fatalf("roll back legacy-table replacement migration: %v", err)
	}
	if err := db.QueryRow("SELECT to_regclass('public.plt_system_dates')::text, to_regclass('public.plt_holiday_calendars')::text").Scan(&legacyDates, &legacyCalendars); err != nil {
		t.Fatal(err)
	}
	if !legacyDates.Valid || !legacyCalendars.Valid {
		t.Fatalf("legacy business-date tables were not restored by rollback: dates=%v calendars=%v", legacyDates, legacyCalendars)
	}
	if _, err := db.Exec("UPDATE plt_system_dates SET current_business_date=DATE '2026-06-30' WHERE branch_code='HEAD_OFFICE'"); err != nil {
		t.Fatalf("update restored legacy row: %v", err)
	}
	var restoredMirror string
	if err := db.QueryRow("SELECT business_date::text FROM plt_business_dates WHERE scope_type='SYSTEM' AND tenant_id IS NULL").Scan(&restoredMirror); err != nil {
		t.Fatal(err)
	}
	if restoredMirror != "2026-06-30" {
		t.Fatalf("restored legacy trigger mirrored business date %s, want 2026-06-30", restoredMirror)
	}
}
