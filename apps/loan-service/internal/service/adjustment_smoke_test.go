package service

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// GATE smoke (adjustment resolve): requires the loan Postgres. Proves the two
// retry-safety fixes end to end:
//
//  1. a restructure retires the previous schedule version instead of
//     deleting it — collected coln_* history survives — while the new active
//     version's principal still sums to the outstanding balance;
//  2. a repeated APPROVE is an idempotent no-op: the side effect (schedule
//     version swap, writeoff balance reduction) is applied exactly once, and
//     a decision against a non-PENDING row is rejected.
//
// Skipped when LOAN_SMOKE_DSN unset.
func TestAdjustmentResolveSmoke(t *testing.T) {
	dsn := os.Getenv("LOAN_SMOKE_DSN")
	if dsn == "" {
		t.Skip("LOAN_SMOKE_DSN not set")
	}
	const tenantID = "00000000-0000-0000-0000-000000000010"

	db, err := sql.Open("pgx/v5", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := migration.Run(db, "postgres"); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	repo := repository.NewLoanRepository(db)
	svc := NewAdjustmentService(repo, &fakeWorkflow{})
	run := time.Now().UTC().Format("20060102T150405.000000000")
	contractCode := "SMKADJ-C-" + run
	agreementCode := "SMKADJ-A-" + run

	if _, err := repo.CreateContract(ctx, &domain.Contract{
		ID:           repository.NewID("ctrt"),
		TenantID:     tenantID,
		ContractCode: contractCode,
		CustomerCode: "SMK-ADJ-CUST",
		ContractDate: "2026-09-07",
		MaturityDate: "2027-09-07",
		LoanAmt:      1_000_000_000,
		LoanTerm:     12,
		TermUnit:     "MONTH",
		Status:       domain.ContractDraft,
		CreatedBy:    "smoke",
	}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := repo.CreateAgreement(ctx, &domain.Agreement{
		ID:            repository.NewID("agr"),
		TenantID:      tenantID,
		ContractCode:  contractCode,
		AgreementCode: agreementCode,
		DisburseDate:  "2026-09-07",
		MaturityDate:  "2027-09-07",
		LoanTerm:      12,
		TermUnit:      "MONTH",
		Status:        "ACTIVE",
		CreatedBy:     "smoke",
	}); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	// Drawdown so the agreement carries a positive outstanding balance.
	if err := repo.SettleRegisterDisbursement(ctx, tenantID, agreementCode, 500_000_000); err != nil {
		t.Fatalf("seed drawdown: %v", err)
	}
	// Seed the first schedule version (12 terms).
	if err := repo.RegeneratePlans(ctx, tenantID, agreementCode, 12, "2026-10-01"); err != nil {
		t.Fatalf("seed schedule v1: %v", err)
	}
	// Mark term 1 of v1 as collected — the history the old DELETE erased.
	var v1FirstID string
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM lnm_repay_plans
		WHERE tenant_id = $1 AND agreement_code = $2 AND is_active
		ORDER BY term_no LIMIT 1`, tenantID, agreementCode).Scan(&v1FirstID); err != nil {
		t.Fatalf("read v1 first row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE lnm_repay_plans SET coln_principal_amt_minor = 7000000, coln_interest_amt_minor = 500000,
		       updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, tenantID, v1FirstID); err != nil {
		t.Fatalf("mark v1 collected: %v", err)
	}

	// ── 1. restructure: new version, history kept, replay is a no-op ──
	agreementRef := agreementCode
	effective := "2026-11-01"
	restructure, err := svc.Create(ctx, "restructure", tenantID, "smoke", &domain.Adjustment{
		ContractCode:  contractCode,
		AgreementCode: &agreementRef,
		EffectiveDate: &effective,
		Payload:       []byte(`{"new_term":6,"new_maturity_date":"2027-05-01"}`),
	})
	if err != nil {
		t.Fatalf("create restructure: %v", err)
	}
	if _, err := svc.Submit(ctx, "restructure", tenantID, "smoke", restructure.ID); err != nil {
		t.Fatalf("submit restructure: %v", err)
	}
	if err := svc.Resolve(ctx, "restructure", tenantID, restructure.ID, "APPROVE", "smoke", "ok"); err != nil {
		t.Fatalf("resolve restructure: %v", err)
	}
	if err := svc.Resolve(ctx, "restructure", tenantID, restructure.ID, "APPROVE", "smoke", "retry"); err != nil {
		t.Fatalf("restructure replay must be an idempotent no-op: %v", err)
	}
	if err := svc.Resolve(ctx, "restructure", tenantID, restructure.ID, "REJECT", "smoke", "late"); err == nil {
		t.Fatal("a different decision after APPROVE must conflict")
	}

	var retired int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM lnm_repay_plans
		WHERE tenant_id = $1 AND agreement_code = $2 AND NOT is_active`, tenantID, agreementCode).Scan(&retired); err != nil {
		t.Fatalf("count retired rows: %v", err)
	}
	if retired != 12 {
		t.Fatalf("retired v1 rows = %d, want 12 (must not be deleted)", retired)
	}
	var keptColn int64
	if err := db.QueryRowContext(ctx, `
		SELECT coln_principal_amt_minor FROM lnm_repay_plans WHERE tenant_id = $1 AND id = $2`,
		tenantID, v1FirstID).Scan(&keptColn); err != nil {
		t.Fatalf("read collected history: %v", err)
	}
	if keptColn != 7_000_000 {
		t.Fatalf("retired row coln_principal = %d, want 7000000 (history preserved)", keptColn)
	}

	var activeCount, totalRows int
	var activeSum int64
	if err := db.QueryRowContext(ctx, `
		SELECT count(*), COALESCE(SUM(plan_principal_amt_minor), 0)
		FROM lnm_repay_plans
		WHERE tenant_id = $1 AND agreement_code = $2 AND is_active`,
		tenantID, agreementCode).Scan(&activeCount, &activeSum); err != nil {
		t.Fatalf("read active version: %v", err)
	}
	if activeCount != 6 {
		t.Fatalf("active rows = %d, want 6", activeCount)
	}
	agreement, err := repo.GetAgreementByCode(ctx, tenantID, agreementCode)
	if err != nil {
		t.Fatalf("reload agreement: %v", err)
	}
	if activeSum != agreement.OutstandingAmt {
		t.Fatalf("active plan principal sum = %d, want outstanding %d", activeSum, agreement.OutstandingAmt)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM lnm_repay_plans WHERE tenant_id = $1 AND agreement_code = $2`,
		tenantID, agreementCode).Scan(&totalRows); err != nil {
		t.Fatalf("count all rows: %v", err)
	}
	if totalRows != 12+6 {
		t.Fatalf("total rows = %d, want 18 (replay must not insert another version)", totalRows)
	}

	// ── 2. writeoff: applied once, replay no-op ──
	before := agreement.OutstandingAmt
	writeoffAmount := int64(50_000_000)
	writeoff, err := svc.Create(ctx, "writeoff", tenantID, "smoke", &domain.Adjustment{
		ContractCode:  contractCode,
		AgreementCode: &agreementRef,
		Amount:        &writeoffAmount,
		Payload:       []byte(`{"reason":"smoke"}`),
	})
	if err != nil {
		t.Fatalf("create writeoff: %v", err)
	}
	if _, err := svc.Submit(ctx, "writeoff", tenantID, "smoke", writeoff.ID); err != nil {
		t.Fatalf("submit writeoff: %v", err)
	}
	if err := svc.Resolve(ctx, "writeoff", tenantID, writeoff.ID, "APPROVE", "smoke", "ok"); err != nil {
		t.Fatalf("resolve writeoff: %v", err)
	}
	if err := svc.Resolve(ctx, "writeoff", tenantID, writeoff.ID, "APPROVE", "smoke", "retry"); err != nil {
		t.Fatalf("writeoff replay must be an idempotent no-op: %v", err)
	}
	after, err := repo.GetAgreementByCode(ctx, tenantID, agreementCode)
	if err != nil {
		t.Fatalf("reload agreement after writeoff: %v", err)
	}
	if after.OutstandingAmt != before-writeoffAmount {
		t.Fatalf("outstanding = %d, want %d (writeoff applied exactly once)", after.OutstandingAmt, before-writeoffAmount)
	}

	// ── 3. a DRAFT adjustment cannot be resolved ──
	draft, err := svc.Create(ctx, "debt-change", tenantID, "smoke", &domain.Adjustment{
		ContractCode:  contractCode,
		AgreementCode: &agreementRef,
		Payload:       []byte(`{"to_debt_group_code":"GROUP_2"}`),
	})
	if err != nil {
		t.Fatalf("create draft debt-change: %v", err)
	}
	if err := svc.Resolve(ctx, "debt-change", tenantID, draft.ID, "APPROVE", "smoke", "early"); err == nil {
		t.Fatal("resolving a DRAFT adjustment must conflict")
	}
}
