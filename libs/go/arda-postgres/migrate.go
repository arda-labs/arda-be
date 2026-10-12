package ardapostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// MigrateOption adjusts Migrate.
type MigrateOption func(*migrateConfig)

type migrateConfig struct {
	allowOutOfOrder bool
	verbose         bool
	lockTimeoutSecs uint64
}

// AllowOutOfOrder applies migrations whose version is older than the newest
// applied one instead of failing. Use it only for a service whose history
// already contains out-of-order versions.
func AllowOutOfOrder() MigrateOption {
	return func(c *migrateConfig) { c.allowOutOfOrder = true }
}

// Quiet suppresses the per-file "OK <file>" log lines.
func Quiet() MigrateOption {
	return func(c *migrateConfig) { c.verbose = false }
}

// WithMigrationLockTimeout sets how long Migrate waits for another process
// holding the migration lock, in seconds. The default is 300.
func WithMigrationLockTimeout(seconds uint64) MigrateOption {
	return func(c *migrateConfig) {
		if seconds > 0 {
			c.lockTimeoutSecs = seconds
		}
	}
}

// Migrate applies every pending goose SQL migration found at the root of fsys
// to a PostgreSQL database, recording versions in goose_db_version.
//
// It uses a goose Provider rather than goose's package-level state, so two
// services or tests in one process cannot overwrite each other's migration
// source. A PostgreSQL session advisory lock serialises concurrent replicas
// that start at the same time; advisory locks are scoped to the current
// database, so services with separate databases never wait on each other.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS, opts ...MigrateOption) error {
	if db == nil {
		return errors.New("migrate: database is nil")
	}
	if fsys == nil {
		return errors.New("migrate: migration filesystem is nil")
	}
	cfg := migrateConfig{verbose: true, lockTimeoutSecs: 300}
	for _, opt := range opts {
		opt(&cfg)
	}
	const probeSeconds = 5
	attempts := cfg.lockTimeoutSecs / probeSeconds
	if attempts == 0 {
		attempts = 1
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(probeSeconds, attempts))
	if err != nil {
		return fmt.Errorf("migrate: session locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithSessionLocker(locker),
		goose.WithAllowOutofOrder(cfg.allowOutOfOrder),
		goose.WithVerbose(cfg.verbose),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
