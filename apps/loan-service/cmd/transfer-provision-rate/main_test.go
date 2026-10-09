package main

import (
	"context"
	"database/sql"
	"testing"

	loanmigration "github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func openPlatformDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t, func(db *sql.DB) error {
		_, err := db.Exec(`CREATE TABLE plt_system_parameters (
			id text PRIMARY KEY, tenant_id text, module text NOT NULL, key text NOT NULL,
			value text NOT NULL, value_type text NOT NULL, unit text,
			scope_type text NOT NULL, scope_id text, effective_from date NOT NULL,
			effective_to date, description text, is_secret boolean NOT NULL DEFAULT false,
			created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now())`)
		if err != nil {
			return err
		}
		_, err = db.Exec(`CREATE UNIQUE INDEX ux_plt_parameters_scope_effective ON plt_system_parameters
			(module,key,scope_type,COALESCE(scope_id,''),COALESCE(tenant_id,''),effective_from)`)
		return err
	})
}

func TestTransferIsIdempotentAndRejectsUnmappedRows(t *testing.T) {
	loanDB := testdb.Open(t, func(db *sql.DB) error { return loanmigration.Run(db, "postgres") })
	platformDB := openPlatformDB(t)
	var localRegistry sql.NullString
	if err := loanDB.QueryRow(`SELECT to_regclass('public.parameter')::text`).Scan(&localRegistry); err != nil {
		t.Fatal(err)
	}
	if localRegistry.Valid {
		t.Fatal("loan-local parameter table should be removed by the owning migration")
	}
	if err := TransferGeneralProvisionRate(context.Background(), loanDB, platformDB); err != nil {
		t.Fatal(err)
	}
	if err := TransferGeneralProvisionRate(context.Background(), loanDB, platformDB); err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	var value string
	if err := platformDB.QueryRow(`SELECT value FROM plt_system_parameters WHERE module='loan' AND key='LNM_GENERAL_PROVISION_RATE' AND scope_type='global'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "0.7500" {
		t.Fatalf("transferred value = %q", value)
	}
	if _, err := loanDB.Exec(`INSERT INTO lnm_general_provision_rates(org_code,rate_percent) VALUES('BR-01',1.25)`); err != nil {
		t.Fatal(err)
	}
	if err := TransferGeneralProvisionRate(context.Background(), loanDB, platformDB); err == nil {
		t.Fatal("unmapped org row must fail closed")
	}
}

func TestTransferRejectsConflictingPlatformValue(t *testing.T) {
	loanDB := testdb.Open(t, func(db *sql.DB) error { return loanmigration.Run(db, "postgres") })
	platformDB := openPlatformDB(t)
	if _, err := platformDB.Exec(`INSERT INTO plt_system_parameters(id,module,key,value,value_type,unit,scope_type,effective_from) VALUES('existing-rate','loan','LNM_GENERAL_PROVISION_RATE','0.5','decimal','percent','global',DATE '0001-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err := TransferGeneralProvisionRate(context.Background(), loanDB, platformDB); err == nil {
		t.Fatal("conflicting Platform value must require reconciliation")
	}
}
