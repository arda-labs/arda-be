package repository

import (
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestLoanStateMachineSchema(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := t.Context()
	const tenantID = "00000000-0000-0000-0000-000000000077"

	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_contracts (id, tenant_id, contract_code, customer_code, status)
		VALUES ('state-contract', $1, 'state-contract', 'customer', 'PENDING_APPROVAL')`, tenantID); err != nil {
		t.Fatalf("insert canonical contract state: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_contracts SET status = 'PENDING' WHERE id = 'state-contract'`); err == nil {
		t.Fatal("legacy contract status PENDING passed CHECK constraint")
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_agreements (id, tenant_id, contract_code, agreement_code, status)
		VALUES ('state-agreement', $1, 'state-contract', 'state-agreement', 'ACTIVE')`, tenantID); err != nil {
		t.Fatalf("insert active agreement: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_agreements SET status = 'PENDING' WHERE id = 'state-agreement'`); err == nil {
		t.Fatal("legacy agreement status PENDING passed CHECK constraint")
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_disbursements (tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor, created_by)
		VALUES ($1, 'state-contract', 'state-agreement', '2026-10-08', 100, 'test')`, tenantID); err != nil {
		t.Fatalf("insert disbursement: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_disbursements SET status = 'SUBMITTED'`); err == nil {
		t.Fatal("legacy disbursement status SUBMITTED passed CHECK constraint")
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_disbursements SET status = 'PENDING_APPROVAL'`); err != nil {
		t.Fatalf("canonical disbursement status rejected: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_collections (tenant_id, contract_code, agreement_code, collection_date, principal_minor, created_by)
		VALUES ($1, 'state-contract', 'state-agreement', '2026-10-08', 10, 'test')`, tenantID); err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_collections SET status = 'PENDING_APPROVAL'`); err != nil {
		t.Fatalf("canonical collection status rejected: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_disbursement_batches (tenant_id, flow_type, txn_date, status)
		VALUES ($1, 'REGISTER', '2026-10-08', 'PENDING_APPROVAL')`, tenantID); err != nil {
		t.Fatalf("insert canonical disbursement batch: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_collection_batches (tenant_id, txn_date, status)
		VALUES ($1, '2026-10-08', 'PENDING_APPROVAL')`, tenantID); err != nil {
		t.Fatalf("insert canonical collection batch: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_repay_plans (id, tenant_id, contract_code, agreement_code, term_no,
			plan_principal_amt_minor, plan_interest_amt_minor, coln_principal_amt_minor)
		VALUES ('state-plan', $1, 'state-contract', 'state-agreement', 1, 100, 20, 30)`, tenantID); err != nil {
		t.Fatalf("insert repayment plan: %v", err)
	}
	var lifecycleStatus, paymentStatus string
	if err := db.QueryRowContext(ctx, `SELECT lifecycle_status, payment_status FROM lnm_repay_plans WHERE id = 'state-plan'`).
		Scan(&lifecycleStatus, &paymentStatus); err != nil {
		t.Fatal(err)
	}
	if lifecycleStatus != "ACTIVE" || paymentStatus != "PARTIALLY_PAID" {
		t.Fatalf("initial repayment plan statuses = %s/%s", lifecycleStatus, paymentStatus)
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_repay_plans SET is_active = FALSE WHERE id = 'state-plan'`); err == nil {
		t.Fatal("active mirror constraint allowed deactivation without SUPERSEDED")
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_repay_plans SET is_active = FALSE, lifecycle_status = 'SUPERSEDED' WHERE id = 'state-plan'`); err != nil {
		t.Fatalf("supersede repayment plan: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE lnm_repay_plans SET coln_principal_amt_minor = 100, coln_interest_amt_minor = 20 WHERE id = 'state-plan'`); err != nil {
		t.Fatalf("mark repayment plan paid by amount: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT payment_status FROM lnm_repay_plans WHERE id = 'state-plan'`).Scan(&paymentStatus); err != nil {
		t.Fatal(err)
	}
	if paymentStatus != "PAID" {
		t.Fatalf("payment status after full collection = %s, want PAID", paymentStatus)
	}
}
