package service

import (
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestParameterRowsStoreModuleUnitAndEffectiveDates(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	_, err := db.Exec(`
		INSERT INTO plt_system_parameters
			(id, tenant_id, key, value, value_type, scope_type, module, unit, effective_from, effective_to)
		VALUES ('param-test', 'tenant-test', 'LNM_GENERAL_PROVISION_RATE', '0.75', 'decimal', 'tenant', 'loan', 'percent', DATE '2026-01-01', DATE '2026-12-31')`)
	if err != nil {
		t.Fatalf("insert effective-dated parameter: %v", err)
	}
	var module, unit string
	var effectiveFrom, effectiveTo string
	if err := db.QueryRow(`SELECT module, unit, effective_from::text, effective_to::text FROM plt_system_parameters WHERE id='param-test'`).Scan(&module, &unit, &effectiveFrom, &effectiveTo); err != nil {
		t.Fatal(err)
	}
	if module != "loan" || unit != "percent" || effectiveFrom != "2026-01-01" || effectiveTo != "2026-12-31" {
		t.Fatalf("metadata = module %q unit %q effective %s..%s", module, unit, effectiveFrom, effectiveTo)
	}
}
