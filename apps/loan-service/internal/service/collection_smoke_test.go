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
	"fmt"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// GATE smoke (P1b.4a collection): requires the loan Postgres. Drives
// create → submit (fake workflow capture) → check → posting detail →
// settle, asserting the agreement outstanding/collected side effects.
// Skipped when LOAN_SMOKE_DSN unset. Posting half is proven by the
// finance PostingSmoke.
func TestCollectionSmoke(t *testing.T) {
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
	contractCode := "SMKC-" + run
	agreementCode := "SMKA-" + run

	contract, err := repo.CreateContract(ctx, &domain.Contract{
		ID:           repository.NewID("ctrt"),
		TenantID:     tenantID,
		ContractCode: contractCode,
		CustomerCode: "SMK-CUST",
		ContractDate: "2026-09-07",
		MaturityDate: "2027-09-07",
		LoanAmt:      1_000_000_000,
		LoanTerm:     12,
		TermUnit:     "MONTH",
		Status:       domain.ContractDraft,
		CreatedBy:    "smoke",
	})
	if err != nil {
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
	// Drawdown first so the collection has outstanding to reduce.
	if err := repo.SettleRegisterDisbursement(ctx, tenantID, agreementCode, 500_000_000); err != nil {
		t.Fatalf("seed drawdown: %v", err)
	}

	fake := &fakeWorkflow{}
	svc := NewCollectionService(repo, fake)

	created, err := svc.Create(ctx, tenantID, "smoke", &domain.Collection{
		ContractCode:   contractCode,
		AgreementCode:  agreementCode,
		CollectionDate: "2026-09-07",
		PrincipalMinor: 100_000_000,
		InterestMinor:  3_500_000,
		CurrencyCode:   "VND",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	submitted, err := svc.Submit(ctx, tenantID, "smoke", created.ID)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.Status != domain.CollectionSubmitted {
		t.Fatalf("status after submit = %q", submitted.Status)
	}
	if len(fake.created) != 1 || fake.created[0].CaseType != CollectionCaseType {
		t.Fatalf("case create mismatch: %+v", fake.created)
	}

	ok, message, err := svc.Check(ctx, tenantID, created.ID)
	if err != nil || !ok {
		t.Fatalf("check: ok=%v message=%q err=%v", ok, message, err)
	}

	detail, err := svc.PostingDetail(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("posting detail: %v", err)
	}
	if detail.GetPrincipalMinor() != 100_000_000 || detail.GetInterestMinor() != 3_500_000 {
		t.Fatalf("detail amounts mismatch: %+v", detail)
	}
	if detail.GetCustomerCode() == "" || detail.GetDebtGroupCode() == "" {
		t.Fatalf("detail analytics incomplete: %+v", detail)
	}

	journalID := fmt.Sprintf("0197c0de-0000-7000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000)
	if err := svc.Settle(ctx, tenantID, created.ID, journalID, "smoke"); err != nil {
		t.Fatalf("settle: %v", err)
	}
	final, err := svc.Get(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if final.Status != domain.CollectionPosted {
		t.Fatalf("final status = %q, want POSTED", final.Status)
	}
	agreement, err := repo.GetAgreementByCode(ctx, tenantID, agreementCode)
	if err != nil {
		t.Fatalf("reload agreement: %v", err)
	}
	if agreement.OutstandingAmt != 400_000_000 {
		t.Fatalf("outstanding = %d, want 400000000", agreement.OutstandingAmt)
	}
	if agreement.ColnPrincipalAmt != 100_000_000 || agreement.ColnInterestAmt != 3_500_000 {
		t.Fatalf("collected counters wrong: principal=%d interest=%d", agreement.ColnPrincipalAmt, agreement.ColnInterestAmt)
	}
	_ = contract
}
