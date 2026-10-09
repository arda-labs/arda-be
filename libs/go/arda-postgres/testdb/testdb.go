// Package testdb provides isolated PostgreSQL databases for integration tests.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const dsnEnvironment = "ARDA_TEST_DSN"

// Open creates a uniquely named disposable database, applies the service's
// migrations, and drops the database when the test completes. ARDA_TEST_DSN
// must point to a PostgreSQL 18 test server and its role must be allowed to
// create and drop databases. With no DSN, the test is skipped with guidance.
func Open(t *testing.T, migrate func(*sql.DB) error) *sql.DB {
	t.Helper()
	dsn := os.Getenv(dsnEnvironment)
	if dsn == "" {
		t.Skip("ARDA_TEST_DSN not set; configure a disposable PostgreSQL 18 server to run database integration tests")
	}
	if migrate == nil {
		t.Fatal("test database requires a migration callback")
	}

	databaseName := newDatabaseName(t)
	adminConfig, err := connectionConfig(dsn, "")
	if err != nil {
		t.Fatal("ARDA_TEST_DSN is not a valid PostgreSQL connection string")
	}
	adminDB := stdlib.OpenDB(*adminConfig)
	adminDB.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := adminDB.PingContext(ctx); err != nil {
		adminDB.Close()
		t.Fatalf("connect to PostgreSQL using ARDA_TEST_DSN: %v", err)
	}
	var serverVersion int
	if err := adminDB.QueryRowContext(ctx, "SELECT current_setting('server_version_num')::integer").Scan(&serverVersion); err != nil {
		adminDB.Close()
		t.Fatalf("read PostgreSQL version: %v", err)
	}
	if serverVersion < 180000 {
		adminDB.Close()
		t.Fatalf("PostgreSQL 18 is required (server_version_num=%d)", serverVersion)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+databaseName); err != nil {
		adminDB.Close()
		t.Fatalf("create isolated test database: %v (the ARDA_TEST_DSN role needs CREATEDB)", err)
	}

	testConfig, err := connectionConfig(dsn, databaseName)
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = adminDB.ExecContext(cleanupCtx, "DROP DATABASE "+databaseName)
		cleanupCancel()
		adminDB.Close()
		t.Fatal("ARDA_TEST_DSN is not a valid PostgreSQL connection string")
	}
	db := stdlib.OpenDB(*testConfig)
	db.SetMaxOpenConns(4)
	t.Cleanup(func() {
		_ = db.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP DATABASE "+databaseName); err != nil {
			t.Errorf("drop isolated test database %s: %v", databaseName, err)
		}
		if err := adminDB.Close(); err != nil {
			t.Errorf("close test database admin connection: %v", err)
		}
	})

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connect to isolated test database: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("apply service migrations to isolated test database: %v", err)
	}
	return db
}

func connectionConfig(dsn, databaseName string) (*pgx.ConnConfig, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if databaseName != "" {
		config.Database = databaseName
		config.RuntimeParams["search_path"] = "public"
	}
	return config, nil
}

func newDatabaseName(t *testing.T) string {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate isolated test database name: %v", err)
	}
	// The identifier is generated from hex only, so it is safe to interpolate
	// into CREATE/DROP DATABASE statements (PostgreSQL does not parameterize DB names).
	return fmt.Sprintf("arda_test_%s", hex.EncodeToString(suffix[:]))
}
