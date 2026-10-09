package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestResolveParameterScopePriorityAndEffectiveDate(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	for _, row := range []struct{ id, value, scope, scopeID, tenant, from, to string }{
		{"global-old", "0.75", "global", "", "", "0001-01-01", ""},
		{"tenant-new", "1.25", "tenant", "", "tenant-1", "2026-01-01", ""},
		{"org-limited", "2.50", "org", "BR-01", "tenant-1", "2026-10-01", "2026-10-31"},
	} {
		_, err := db.Exec(`INSERT INTO plt_system_parameters (id,tenant_id,module,key,value,value_type,unit,scope_type,scope_id,effective_from,effective_to)
			VALUES ($1,NULLIF($2,''),'loan','LNM_RATE',$3,'decimal','percent',$4,NULLIF($5,''),$6::date,NULLIF($7,'')::date)`, row.id, row.tenant, row.value, row.scope, row.scopeID, row.from, row.to)
		if err != nil {
			t.Fatal(err)
		}
	}
	svc := NewPlatformService(repository.NewPlatformRepository(db))
	date := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	scopes := []ScopeSelector{{TenantID: "tenant-1", ScopeType: "global"}, {TenantID: "tenant-1", ScopeType: "tenant"}, {TenantID: "tenant-1", ScopeType: "org", ScopeID: "BR-01"}}
	got, err := svc.ResolveParameter(context.Background(), "tenant-1", "loan", "LNM_RATE", scopes, date)
	if err != nil || got.Value != "2.50" {
		t.Fatalf("org resolution = %q, %v", got.Value, err)
	}
	date = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	got, err = svc.ResolveParameter(context.Background(), "tenant-1", "loan", "LNM_RATE", scopes, date)
	if err != nil || got.Value != "1.25" {
		t.Fatalf("expired org fallback = %q, %v", got.Value, err)
	}
	got, err = svc.ResolveParameter(context.Background(), "tenant-2", "loan", "LNM_RATE", []ScopeSelector{{ScopeType: "global"}}, date)
	if err != nil || got.Value != "0.75" {
		t.Fatalf("global fallback = %q, %v", got.Value, err)
	}
}
