package migration

import (
	"database/sql"
	"fmt"

	"github.com/arda-labs/arda/apps/platform-service/migrations"
	"github.com/pressly/goose/v3"
)

// Run applies pending migrations from the embedded migrations directory.
func Run(db *sql.DB, dialect string) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.Up(db, ".", goose.WithAllowMissing()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
