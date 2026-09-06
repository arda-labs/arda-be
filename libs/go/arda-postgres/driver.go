package ardapostgres

import (
	"database/sql"

	"github.com/jackc/pgx/v5/pgtype"
)

// Driver namespaces database/sql value helpers for the pgx driver. The
// methods are Go 1.27 generic methods (type parameters declared on the
// method itself), keeping pgx-encoding rules in one place instead of free
// functions duplicated per repository package.
var Driver driverHelpers

type driverHelpers struct{}

// NotNil replaces a nil slice with an empty one. pgx encodes nil slices as
// SQL NULL while lib/pq (the previous driver) encoded them as '{}'; columns
// populated before the pgx migration rely on the '{}' semantics. Call sites
// should wrap every slice passed as a query argument.
func (driverHelpers) NotNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

// Scanner adapts a pgtype-decodable value (e.g. *[]string for a text[]
// column) to sql.Scanner for database/sql Scan destinations. database/sql
// cannot scan PostgreSQL array values into plain slices without it.
func (driverHelpers) Scanner(v any) sql.Scanner {
	return pgtype.NewMap().SQLScanner(v)
}
