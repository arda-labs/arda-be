package ardapostgres

import (
	"database/sql"
	"errors"
	"testing"
)

type versionedRow struct {
	version int64
	err     error
}

func (r versionedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*int64) = r.version
	return nil
}

func TestUpdateVersionReturnsNewVersion(t *testing.T) {
	got, err := UpdateVersion(versionedRow{version: 8})
	if err != nil || got != 8 {
		t.Fatalf("UpdateVersion() = %d, %v; want 8, nil", got, err)
	}
}

func TestUpdateVersionMapsNoRowsToStale(t *testing.T) {
	_, err := UpdateVersion(versionedRow{err: sql.ErrNoRows})
	if !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("UpdateVersion() error = %v; want ErrStaleVersion", err)
	}
}

func TestUpdateVersionReturnsQueryError(t *testing.T) {
	want := errors.New("database unavailable")
	_, err := UpdateVersion(versionedRow{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("UpdateVersion() error = %v; want %v", err, want)
	}
}

func TestUpdateVersionRejectsNilRow(t *testing.T) {
	if _, err := UpdateVersion(nil); err == nil {
		t.Fatal("UpdateVersion(nil) returned nil error")
	}
}
