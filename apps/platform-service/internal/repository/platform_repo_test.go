package repository

import (
	"database/sql"
	"errors"
	"testing"
)

func TestRequireTenantID(t *testing.T) {
	for _, tenantID := range []string{"", " ", "\t"} {
		if err := requireTenantID(tenantID); err == nil {
			t.Fatalf("requireTenantID(%q) accepted an empty scope", tenantID)
		}
	}
	if err := requireTenantID("tenant-1"); err != nil {
		t.Fatalf("requireTenantID rejected a valid scope: %v", err)
	}
}

type fakeResult struct {
	affected int64
	err      error
}

func (f fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (f fakeResult) RowsAffected() (int64, error) { return f.affected, f.err }

// Delete/update-by-id must surface 0 rows as ErrNotFound so the handler can
// answer 404 instead of {"ok": true}.
func TestAffectedOrNotFound(t *testing.T) {
	dbErr := errors.New("connection reset")
	if err := affectedOrNotFound(fakeResult{}, dbErr); !errors.Is(err, dbErr) {
		t.Fatalf("driver error = %v, want %v", err, dbErr)
	}
	if err := affectedOrNotFound(fakeResult{affected: 0}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("0 rows = %v, want ErrNotFound", err)
	}
	if err := affectedOrNotFound(fakeResult{affected: 1}, nil); err != nil {
		t.Fatalf("1 row returned %v, want nil", err)
	}
	rowsErr := errors.New("rows affected unavailable")
	if err := affectedOrNotFound(fakeResult{err: rowsErr}, nil); !errors.Is(err, rowsErr) {
		t.Fatalf("RowsAffected error = %v, want %v", err, rowsErr)
	}
}

var _ sql.Result = fakeResult{}
