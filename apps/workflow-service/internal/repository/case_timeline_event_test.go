package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/migration"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// AddTimelineEvent used the case id both as an INSERT ... SELECT output column
// and in an `id = $2` predicate, so PostgreSQL deduced two different types for
// the same parameter (SQLSTATE 42P08) and every checker decision timeline row
// was dropped. This runs the statement against the real migrated schema.
func TestAddTimelineEventWritesRowForOwnedCase(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	seedTimelineCase(t, db, "case-timeline-1", "tenant-1")
	seedTimelineCase(t, db, "case-timeline-2", "tenant-2")
	repo := NewCaseRepository(db)
	ctx := ardametadata.AppendToOutgoing(context.Background(), ardametadata.Context{TenantID: "tenant-1"})

	if err := repo.AddTimelineEvent(ctx, "case-timeline-1", "CHECKER_APPROVED", "Approved"); err != nil {
		t.Fatalf("AddTimelineEvent: %v", err)
	}

	var eventType, note string
	if err := db.QueryRow(
		`SELECT event_type, note FROM case_timeline_events WHERE case_id = 'case-timeline-1'`,
	).Scan(&eventType, &note); err != nil {
		t.Fatalf("timeline row missing: %v", err)
	}
	if eventType != "CHECKER_APPROVED" || note != "Approved" {
		t.Fatalf("timeline row = (%q, %q), want (CHECKER_APPROVED, Approved)", eventType, note)
	}
}

// The tenant predicate must still keep a case id from crossing tenants: the
// call succeeds but writes nothing for another tenant's case.
func TestAddTimelineEventIgnoresOtherTenantsCase(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	seedTimelineCase(t, db, "case-timeline-2", "tenant-2")
	repo := NewCaseRepository(db)
	ctx := ardametadata.AppendToOutgoing(context.Background(), ardametadata.Context{TenantID: "tenant-1"})

	if err := repo.AddTimelineEvent(ctx, "case-timeline-2", "CHECKER_APPROVED", "Approved"); err != nil {
		t.Fatalf("AddTimelineEvent: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM case_timeline_events WHERE case_id = 'case-timeline-2'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("timeline rows for another tenant's case = %d, want 0", n)
	}
}

func seedTimelineCase(t *testing.T, db *sql.DB, id, tenant string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO business_cases (id, tenant_id, case_type, case_code, title,
			primary_object_type, primary_object_id, domain_service, status, priority, created_by)
		VALUES ($1, $2, 'DPM_PRODUCT_REGISTER_V1', $3, 'Timeline test', 'object', $1, 'test-service', 'IN_REVIEW', 'NORMAL', 'maker-1')
	`, id, tenant, "TL-"+id); err != nil {
		t.Fatalf("seed case %s: %v", id, err)
	}
}
