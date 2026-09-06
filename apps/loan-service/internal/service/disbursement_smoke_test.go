package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// GATE smoke (P1b disbursement): requires the loan Postgres. Runs migrations,
// seeds contract + agreement, then drives create → submit (fake workflow
// capture) → check → posting detail → settle, asserting the agreement
// outstanding + contract ACTIVE side effects. Skipped when LOAN_SMOKE_DSN unset.
//
// The posting half (PostTransaction) is proven by the finance PostingSmoke;
// Zeebe routing is exercised by the CRM v2 flow. Together they cover the
// worker path this test simulates.
type fakeWorkflow struct {
	created   []struct {
		CaseType  string
		PrimaryID string
	}
	submitted []string
	caseID    string
}

func (f *fakeWorkflow) CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	f.created = append(f.created, struct {
		CaseType  string
		PrimaryID string
	}{in.CaseType, in.PrimaryObjectID})
	// UUID-shaped and unique per submit, like the real workflow service.
	f.caseID = fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000)
	return &workflowv1.BusinessCase{Id: f.caseID}, nil
}

func (f *fakeWorkflow) SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error) {
	f.submitted = append(f.submitted, caseID)
	return &workflowv1.BusinessCase{Id: caseID}, nil
}

func TestDisbursementSmoke(t *testing.T) {
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
	run := time.Now().UTC().Format("20060102T150405")
	contractCode := "SMK-C-" + run
	agreementCode := "SMK-A-" + run

	contract, err := repo.CreateContract(ctx, &domain.Contract{
		ID:           repository.NewID("ctrt"),
		TenantID:     tenantID,
		ContractCode: contractCode,
		CustomerCode: "SMK-CUST",
		LoanAmt:      1_000_000_000,
		LoanTerm:     12,
		TermUnit:     "MONTH",
		ContractDate: "2026-09-07",
		MaturityDate: "2027-09-07",
		Status:       domain.ContractDraft,
		CreatedBy:    "smoke",
	})
	if err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := repo.CreateAgreement(ctx, &domain.Agreement{
		ID:               repository.NewID("agr"),
		TenantID:         tenantID,
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		DisburseDate:     "2026-09-07",
		DisburseAmt:      0,
		LoanTerm:         12,
		TermUnit:         "MONTH",
		MaturityDate:     "2027-09-07",
		Status:           "PENDING",
		CreatedBy:        "smoke",
	}); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}

	fake := &fakeWorkflow{}
	svc := NewDisbursementService(repo, fake)

	// 1. Create DRAFT.
	created, err := svc.Create(ctx, tenantID, "smoke", &domain.Disbursement{
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		DisburseDate:     "2026-09-07",
		DisburseAmtMinor: 500_000_000,
		CurrencyCode:     "VND",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Status != domain.DisbursementDraft {
		t.Fatalf("status = %q, want DRAFT", created.Status)
	}

	// 2. Submit creates + submits a LNM_DISBURSEMENT_V2 case.
	submitted, err := svc.Submit(ctx, tenantID, "smoke", created.ID)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.Status != domain.DisbursementSubmitted {
		t.Fatalf("status after submit = %q", submitted.Status)
	}
	if len(fake.created) != 1 || fake.created[0].CaseType != "LNM_DISBURSEMENT_V2" {
		t.Fatalf("case create mismatch: %+v", fake.created)
	}
	if len(fake.submitted) != 1 {
		t.Fatalf("case submit missing")
	}

	// 3. Check passes for SUBMITTED.
	ok, message, err := svc.Check(ctx, tenantID, created.ID)
	if err != nil || !ok {
		t.Fatalf("check: ok=%v message=%q err=%v", ok, message, err)
	}

	// 4. PostingDetail carries the analytics the worker needs.
	detail, err := svc.PostingDetail(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("posting detail: %v", err)
	}
	if detail.GetDisburseAmtMinor() != created.DisburseAmtMinor || detail.GetAgreementCode() != agreementCode {
		t.Fatalf("detail mismatch: %+v", detail)
	}
	if detail.GetDebtGroupCode() == "" || detail.GetCustomerCode() == "" {
		t.Fatalf("detail analytics incomplete: %+v", detail)
	}

	// 5. Settle posts status + outstanding side effects.
	journalID := fmt.Sprintf("0197c0de-0000-7000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000) // journal entry UUID shape
	if err := svc.Settle(ctx, tenantID, created.ID, journalID, "smoke"); err != nil {
		t.Fatalf("settle: %v", err)
	}
	final, err := svc.Get(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if final.Status != domain.DisbursementPosted {
		t.Fatalf("final status = %q, want POSTED", final.Status)
	}
	if final.JournalEntryID == nil || *final.JournalEntryID != journalID {
		t.Fatalf("journal entry id not stored: %+v", final.JournalEntryID)
	}
	agreement, err := repo.GetAgreementByCode(ctx, tenantID, agreementCode)
	if err != nil {
		t.Fatalf("reload agreement: %v", err)
	}
	if agreement.OutstandingAmt != created.DisburseAmtMinor {
		t.Fatalf("outstanding = %d, want %d", agreement.OutstandingAmt, created.DisburseAmtMinor)
	}
	var contractStatus string
	if err := db.QueryRow(`SELECT status FROM lnm_contracts WHERE id = $1`, contract.ID).Scan(&contractStatus); err != nil {
		t.Fatalf("reload contract: %v", err)
	}
	if contractStatus != "ACTIVE" {
		t.Fatalf("contract status = %q, want ACTIVE after first drawdown", contractStatus)
	}
}
