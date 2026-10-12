package ardapostgres_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	"github.com/pressly/goose/v3"
)

func sqlMigration(up string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("-- +goose Up\n" + up + "\n-- +goose Down\nSELECT 1;\n")}
}

func emptyDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t, func(*sql.DB) error { return nil })
}

func appliedVersions(t *testing.T, db *sql.DB) []int64 {
	t.Helper()
	rows, err := db.Query(`SELECT version_id FROM goose_db_version WHERE version_id > 0 AND is_applied ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	return versions
}

func TestMigrateAppliesOnceAndIsIdempotent(t *testing.T) {
	db := emptyDB(t)
	fsys := fstest.MapFS{
		"20260101000001_items.sql": sqlMigration("CREATE TABLE items (id INT PRIMARY KEY);"),
		"20260101000002_seed.sql":  sqlMigration("INSERT INTO items VALUES (1);"),
	}
	for range 2 {
		if err := ardapostgres.Migrate(context.Background(), db, fsys, ardapostgres.Quiet()); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("seed applied %d times, want once", n)
	}
	if got := appliedVersions(t, db); len(got) != 2 {
		t.Fatalf("applied versions = %v", got)
	}
}

func TestMigrateOutOfOrderNeedsExplicitOption(t *testing.T) {
	db := emptyDB(t)
	fsys := fstest.MapFS{
		"20260101000001_a.sql": sqlMigration("CREATE TABLE a (id INT);"),
		"20260101000003_c.sql": sqlMigration("CREATE TABLE c (id INT);"),
	}
	if err := ardapostgres.Migrate(context.Background(), db, fsys, ardapostgres.Quiet()); err != nil {
		t.Fatal(err)
	}
	fsys["20260101000002_b.sql"] = sqlMigration("CREATE TABLE b (id INT);")

	err := ardapostgres.Migrate(context.Background(), db, fsys, ardapostgres.Quiet())
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("strict migrate with a missing version = %v, want a missing-migration error", err)
	}
	if err := ardapostgres.Migrate(context.Background(), db, fsys, ardapostgres.Quiet(), ardapostgres.AllowOutOfOrder()); err != nil {
		t.Fatal(err)
	}
	if got := appliedVersions(t, db); len(got) != 3 || got[2] != 20260101000002 {
		t.Fatalf("applied versions = %v, want the missing version applied last", got)
	}
}

func TestMigrateSerialisesConcurrentReplicas(t *testing.T) {
	db := emptyDB(t)
	fsys := fstest.MapFS{
		// Plain CREATE TABLE fails if two replicas both run it.
		"20260101000001_once.sql": sqlMigration("CREATE TABLE once (id INT); SELECT pg_sleep(0.5);"),
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			errs <- ardapostgres.Migrate(context.Background(), db, fsys, ardapostgres.Quiet(), ardapostgres.WithMigrationLockTimeout(30))
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate: %v", err)
		}
	}
	if got := appliedVersions(t, db); len(got) != 1 {
		t.Fatalf("applied versions = %v, want exactly one", got)
	}
}

// TestMigrateContinuesDatabaseMigratedByGlobalGoose proves a database whose
// history was written by goose's package-level API (the previous per-service
// runner) is picked up without re-running anything.
func TestMigrateContinuesDatabaseMigratedByGlobalGoose(t *testing.T) {
	db := emptyDB(t)
	legacy := fstest.MapFS{
		"20260101000001_items.sql": sqlMigration("CREATE TABLE items (id INT PRIMARY KEY);"),
	}
	goose.SetBaseFS(legacy)
	t.Cleanup(func() { goose.SetBaseFS(nil) })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatal(err)
	}

	next := fstest.MapFS{
		"20260101000001_items.sql": legacy["20260101000001_items.sql"],
		"20260101000002_more.sql":  sqlMigration("ALTER TABLE items ADD COLUMN name TEXT;"),
	}
	if err := ardapostgres.Migrate(context.Background(), db, next, ardapostgres.Quiet()); err != nil {
		t.Fatal(err)
	}
	if got := appliedVersions(t, db); len(got) != 2 || got[0] != 20260101000001 || got[1] != 20260101000002 {
		t.Fatalf("applied versions = %v", got)
	}
}

func TestMigrateRejectsNilInputs(t *testing.T) {
	if err := ardapostgres.Migrate(context.Background(), nil, fstest.MapFS{}); err == nil {
		t.Fatal("nil db accepted")
	}
	if err := ardapostgres.Migrate(context.Background(), &sql.DB{}, nil); err == nil {
		t.Fatal("nil fs accepted")
	}
}
