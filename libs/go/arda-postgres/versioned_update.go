package ardapostgres

import (
	"database/sql"
	"errors"
)

// ErrStaleVersion means an optimistic-lock update matched no row because the
// record was deleted or its version changed since it was read.
var ErrStaleVersion = errors.New("record changed since it was read")

// VersionRow is the result returned by database/sql QueryRowContext.
type VersionRow interface {
	Scan(dest ...any) error
}

// UpdateVersion scans the version returned by an optimistic-lock UPDATE and
// maps sql.ErrNoRows to ErrStaleVersion. The SQL statement must increment the
// version and guard the write, for example:
//
// UPDATE some_table
// SET value = $3, version = version + 1
// WHERE id = $1 AND version = $2
// RETURNING version
func UpdateVersion(row VersionRow) (int64, error) {
	if row == nil {
		return 0, errors.New("versioned update row is nil")
	}
	var version int64
	if err := row.Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrStaleVersion
		}
		return 0, err
	}
	return version, nil
}
