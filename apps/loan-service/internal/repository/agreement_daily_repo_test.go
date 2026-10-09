package repository

import (
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestCaptureAndListAgreementDailySnapshots(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	const tenantID = "00000000-0000-0000-0000-000000000045"
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO lnm_agreements (
			id, tenant_id, contract_code, agreement_code, currency_code,
			disburse_amt_minor, pending_disburse_amt_minor, outstanding_amt_minor,
			coln_principal_amt_minor, coln_interest_amt_minor, provision_amt_minor,
			off_bal_principal_minor, off_bal_interest_minor, off_bal_due_interest_minor
		) VALUES ('agreement-snapshot-1', $1, 'contract-snapshot-1', 'agreement-snapshot-1', 'VND',
			1000, 100, 800, 50, 30, 20, 10, 5, 2)`, tenantID); err != nil {
		t.Fatal(err)
	}

	repo := NewLoanRepository(db)
	if count, err := repo.CaptureAgreementDailySnapshots(t.Context(), tenantID, "2026-10-07"); err != nil || count != 1 {
		t.Fatalf("first capture count = %d, err = %v; want 1", count, err)
	}
	snapshots, err := repo.ListAgreementDailySnapshots(t.Context(), tenantID, "2026-10-07")
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("ListAgreementDailySnapshots() = %#v, %v; want one row", snapshots, err)
	}
	if got := snapshots[0]; got.OutstandingAmtMinor != 800 || got.OffBalPrincipalMinor != 10 || got.OffBalInterestMinor != 5 || got.OffBalDueInterestMinor != 2 {
		t.Fatalf("captured balances = %#v", got)
	}

	if _, err := db.ExecContext(t.Context(), `UPDATE lnm_agreements SET outstanding_amt_minor = 700, off_bal_interest_minor = 4 WHERE id = 'agreement-snapshot-1'`); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.CaptureAgreementDailySnapshots(t.Context(), tenantID, "2026-10-07"); err != nil || count != 1 {
		t.Fatalf("retry capture count = %d, err = %v; want one upsert", count, err)
	}
	snapshots, err = repo.ListAgreementDailySnapshots(t.Context(), tenantID, "2026-10-07")
	if err != nil || len(snapshots) != 1 || snapshots[0].OutstandingAmtMinor != 700 || snapshots[0].OffBalInterestMinor != 4 {
		t.Fatalf("retry snapshot = %#v, %v; want updated same-date row", snapshots, err)
	}
	otherTenant, err := repo.ListAgreementDailySnapshots(t.Context(), "00000000-0000-0000-0000-000000000046", "2026-10-07")
	if err != nil || len(otherTenant) != 0 {
		t.Fatalf("other tenant snapshots = %#v, %v; want empty", otherTenant, err)
	}
	nextDate, err := repo.ListAgreementDailySnapshots(t.Context(), tenantID, "2026-10-08")
	if err != nil || len(nextDate) != 0 {
		t.Fatalf("missing-date snapshots = %#v, %v; must not substitute a previous date", nextDate, err)
	}
}
