package repository

import (
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestAgreementDailySnapshotSchema(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()

	for _, column := range []string{
		"off_bal_principal_minor",
		"off_bal_interest_minor",
		"off_bal_due_interest_minor",
		"extension_date",
		"overdue_reason",
	} {
		var exists bool
		if err := db.QueryRowContext(t.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = 'lnm_agreements' AND column_name = $1
			)`, column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("lnm_agreements.%s is missing", column)
		}
	}

	for _, table := range []string{"lnm_journal_link", "lnm_agreement_daily"} {
		var exists bool
		if err := db.QueryRowContext(t.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema() AND table_name = $1
			)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s is missing", table)
		}
	}

	const tenantID = "00000000-0000-0000-0000-000000000044"
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO lnm_agreements (id, tenant_id, contract_code, agreement_code, currency_code)
		VALUES ('agreement-daily-1', $1, 'contract-daily-1', 'agreement-daily-1', 'VND')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
		UPDATE lnm_agreements SET off_bal_interest_minor = -1
		WHERE tenant_id = $1 AND id = 'agreement-daily-1'`, tenantID); err == nil {
		t.Fatal("negative off-balance balance unexpectedly passed its CHECK constraint")
	}

	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO lnm_journal_link
			(tenant_id, business_txn_type, business_txn_id, journal_entry_id, posted_at, status)
		VALUES ($1, 'COLLECTION', 'txn-daily-1', uuidv7(), now(), 'POSTED')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO lnm_journal_link
			(tenant_id, business_txn_type, business_txn_id, journal_entry_id, posted_at, status)
		VALUES ($1, 'COLLECTION', 'txn-daily-1', uuidv7(), now(), 'POSTED')`, tenantID); err == nil {
		t.Fatal("duplicate business transaction unexpectedly created a second journal link")
	}
}
