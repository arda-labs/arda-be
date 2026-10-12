// Package migration applies the service's embedded goose migrations on
// startup through the shared runner in libs/go/arda-postgres.
package migration

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arda-labs/arda/apps/deposit-service/migrations"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
)

// Run applies pending migrations from the embedded migrations directory.
func Run(db *sql.DB, dialect string) error {
	if dialect != "postgres" {
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	return ardapostgres.Migrate(context.Background(), db, migrations.FS)
}
