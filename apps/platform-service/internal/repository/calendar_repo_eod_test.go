package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestCompleteEODAdvancesDateAndRunInOneTransaction(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	ctx := context.Background()
	var id string
	var current, next string
	if err := db.QueryRowContext(ctx, `SELECT id,business_date::text,next_business_date::text
		FROM plt_business_dates WHERE scope_type='SYSTEM' AND tenant_id IS NULL`).Scan(&id, &current, &next); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO plt_eod_runs(eod_date,scope_type,status)
		VALUES($1::date,'SYSTEM','RUNNING')`, current); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE plt_business_dates SET status='EOD_PROCESSING' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	newNext := mustDate(t, next).AddDate(0, 0, 1)
	if _, err := NewCalendarRepository(db).CompleteEOD(ctx, current, mustDate(t, next), newNext); err != nil {
		t.Fatalf("complete EOD: %v", err)
	}
	var gotCurrent, gotPrevious, runStatus string
	if err := db.QueryRowContext(ctx, `SELECT business_date::text,prev_business_date::text FROM plt_business_dates WHERE id=$1`, id).Scan(&gotCurrent, &gotPrevious); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM plt_eod_runs WHERE eod_date=$1::date`, current).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if gotCurrent != next || gotPrevious != current || runStatus != "SUCCEEDED" {
		t.Fatalf("transition = current %s previous %s run %s; want %s %s SUCCEEDED", gotCurrent, gotPrevious, runStatus, next, current)
	}
}

func TestCompleteEODDoesNotAdvanceWithoutRunningEODRun(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	ctx := context.Background()
	var id, current, next string
	if err := db.QueryRowContext(ctx, `SELECT id,business_date::text,next_business_date::text
		FROM plt_business_dates WHERE scope_type='SYSTEM' AND tenant_id IS NULL`).Scan(&id, &current, &next); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE plt_business_dates SET status='EOD_PROCESSING' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCalendarRepository(db).CompleteEOD(ctx, current, mustDate(t, next), mustDate(t, next).AddDate(0, 0, 1)); err == nil {
		t.Fatal("completion succeeded without a RUNNING EOD run")
	}
	var gotCurrent, status string
	if err := db.QueryRowContext(ctx, `SELECT business_date::text,status FROM plt_business_dates WHERE id=$1`, id).Scan(&gotCurrent, &status); err != nil {
		t.Fatal(err)
	}
	if gotCurrent != current || status != "EOD_PROCESSING" {
		t.Fatalf("failed completion changed date state: date=%s status=%s", gotCurrent, status)
	}
}

func mustDate(t *testing.T, value string) time.Time {
	t.Helper()
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
