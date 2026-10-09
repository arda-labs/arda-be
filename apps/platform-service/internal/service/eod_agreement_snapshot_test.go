package service

import (
	"database/sql"
	"log/slog"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestSeedJobsRegistersLoanAgreementDailySnapshot(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()

	const tenantID = "00000000-0000-0000-0000-000000000044"
	svc := NewEODService(db, slog.Default(), nil, nil)
	if err := svc.SeedJobs(t.Context(), tenantID); err != nil {
		t.Fatal(err)
	}

	var module, endpoint string
	var dependencies string
	var sequence int
	if err := db.QueryRowContext(t.Context(), `
		SELECT module, endpoint, sequence, depends_on::text
		FROM plt_job_definitions WHERE tenant_id = $1 AND code = 'LNM_AGREEMENT_DAILY_SNAPSHOT'`, tenantID).
		Scan(&module, &endpoint, &sequence, &dependencies); err != nil {
		t.Fatalf("seed daily agreement snapshot EOD step: %v", err)
	}
	if module != "loan" || endpoint != "http://loan-service:8080/internal/jobs/agreement-daily-snapshot" {
		t.Fatalf("snapshot step route = %q %q", module, endpoint)
	}
	if sequence <= 20 || dependencies != "{LNM_PROVISION_DAILY}" {
		t.Fatalf("snapshot step ordering = sequence %d, dependencies %v", sequence, dependencies)
	}
}
