package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/paramspec"
	ardaParams "github.com/arda-labs/arda/libs/go/arda-params"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestParameterRegistryResolvesScopeAndEffectiveDate(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	registry := ardaParams.NewRegistry(db)
	if err := registry.Declare(paramspec.LoanModule()); err != nil {
		t.Fatal(err)
	}
	if err := registry.Verify(context.Background()); err != nil {
		t.Fatalf("verify migrated global rate: %v", err)
	}
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	key := ardaParams.ScopeKey{TenantID: "00000000-0000-0000-0000-000000000010", OrgCode: "BR-01", EffectiveDate: day}
	get := func() float64 {
		t.Helper()
		value, err := ardaParams.Get[float64](context.Background(), registry, "loan", paramspec.GeneralProvisionRate, key)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if got := get(); got != 0.75 {
		t.Fatalf("migrated GLOBAL = %v, want 0.75", got)
	}

	insert := func(scope, tenant, org, valueType, value string, from, to any) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO parameter(tenant_id,module,code,scope,org_code,value_type,value,unit,effective_from,effective_to) VALUES(NULLIF($1,''),'loan',$2,$3,NULLIF($4,''),$5,$6,'percent',$7,$8)`, tenant, paramspec.GeneralProvisionRate, scope, org, valueType, value, from, to)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("TENANT", key.TenantID, "", "DECIMAL", "1.25", "2026-01-01", nil)
	if got := get(); got != 1.25 {
		t.Fatalf("TENANT = %v, want 1.25", got)
	}
	insert("ORG", key.TenantID, key.OrgCode, "DECIMAL", "2.5", "2026-10-01", "2026-10-31")
	if got := get(); got != 2.5 {
		t.Fatalf("ORG = %v, want 2.5", got)
	}
	key.EffectiveDate = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if got := get(); got != 1.25 {
		t.Fatalf("expired ORG must resolve TENANT, got %v", got)
	}
	key.TenantID = "00000000-0000-0000-0000-000000000011"
	if got := get(); got != 0.75 {
		t.Fatalf("other tenant must resolve GLOBAL, got %v", got)
	}
	key.EffectiveDate = time.Time{}
	if _, err := ardaParams.Get[float64](context.Background(), registry, "loan", paramspec.GeneralProvisionRate, key); !errors.Is(err, ardaParams.ErrEffectiveDateRequired) {
		t.Fatalf("missing effective date error = %v", err)
	}
}

func TestParameterRegistryMissingAndTypeMismatch(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	registry := ardaParams.NewRegistry(db)
	if err := registry.Declare(paramspec.LoanModule()); err != nil {
		t.Fatal(err)
	}
	key := ardaParams.ScopeKey{TenantID: "00000000-0000-0000-0000-000000000010", EffectiveDate: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	if _, err := ardaParams.Get[float64](context.Background(), registry, "loan", "UNKNOWN", key); !errors.Is(err, ardaParams.ErrParamUndeclared) {
		t.Fatalf("undeclared error = %v", err)
	}
	if _, err := db.Exec(`DELETE FROM parameter WHERE module='loan' AND code=$1`, paramspec.GeneralProvisionRate); err != nil {
		t.Fatal(err)
	}
	if _, err := ardaParams.Get[float64](context.Background(), registry, "loan", paramspec.GeneralProvisionRate, key); !errors.Is(err, ardaParams.ErrParamMissing) {
		t.Fatalf("missing error = %v", err)
	} else {
		var typed *ardaParams.ParamError
		if !errors.As(err, &typed) || typed.Code != "PARAM_MISSING" {
			t.Fatalf("missing error code = %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO parameter(module,code,scope,value_type,value,unit,effective_from) VALUES('loan',$1,'GLOBAL','STRING','0.75','percent','2026-01-01')`, paramspec.GeneralProvisionRate); err != nil {
		t.Fatal(err)
	}
	if _, err := ardaParams.Get[float64](context.Background(), registry, "loan", paramspec.GeneralProvisionRate, key); !errors.Is(err, ardaParams.ErrTypeMismatch) {
		t.Fatalf("wrong type error = %v", err)
	}
}

func TestParameterRegistryVerifiesDeclaredCodeItems(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	registry := ardaParams.NewRegistry(db)
	spec := ardaParams.ModuleSpec{Name: "test-catalog", CodeSets: []ardaParams.CodeSetSpec{{Code: "LNM_TEST", Items: []ardaParams.CodeItemSpec{{Code: "ACTIVE", Name: "Active", Sort: 1, Active: true}}}}}
	if err := registry.Declare(spec); err != nil {
		t.Fatal(err)
	}
	if err := registry.Verify(context.Background()); err == nil {
		t.Fatal("missing declared code set must fail verification")
	}
	if _, err := db.Exec(`INSERT INTO code_set(code,name) VALUES('LNM_TEST','Test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO code_item(set_code,code,name,sort,active) VALUES('LNM_TEST','ACTIVE','Active',1,true)`); err != nil {
		t.Fatal(err)
	}
	if err := registry.Verify(context.Background()); err != nil {
		t.Fatal(fmt.Errorf("verify seeded code set: %w", err))
	}
}
