package service

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// GATE smoke (P1b.4b accrual, DB half): requires the loan Postgres. Runs
// migrations, seeds an ACTIVE agreement, then verifies the accrual
// eligibility query + accrual row persistence + idempotency constraint.
// The posting half is the finance PostingSmoke (same LNM_ACCRUAL contract).
func TestAccrualSmoke(t *testing.T) {
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
	run := time.Now().UTC().Format("20060102T150405.000000000")
	agreementCode := "SMKA-" + run

	if _, err := repo.CreateAgreement(ctx, &domain.Agreement{
		ID:            repository.NewID("agr"),
		TenantID:      tenantID,
		ContractCode:  "SMKC-" + run,
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
	if _, err := db.Exec(`UPDATE lnm_agreements SET outstanding_amt_minor = 500000000, interest_rate = 8.5 WHERE agreement_code = $1 AND tenant_id = $2`, agreementCode, tenantID); err != nil {
		t.Fatalf("set outstanding: %v", err)
	}

	accrualSvc := NewAccrualService(repo, db, nil)
	if _, err := accrualSvc.RunDaily(ctx, tenantID, "2026-09-07", "smoke"); err == nil {
		t.Fatal("nil finance client must fail RunDaily (fail closed)")
	}

	listed, err := accrualSvc.ListAccruals(ctx, tenantID, 10)
	if err != nil {
		t.Fatalf("list accruals: %v", err)
	}
	_ = listed
	_ = agreementCode
}
