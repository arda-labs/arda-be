package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// GATE smoke (P1b v2 disbursement): requires the loan Postgres. Runs
// migrations, seeds contract + agreement, then drives the two-flow EPAS
// LNM.300.02 shape — REGISTER create → submit (fake workflow capture) → check
// → posting detail → SettleRegister (outstanding + pending bump), then
// COMPLETE create(source=register) → submit → SettleComplete (pending unwind,
// contract ACTIVE on first completion) — plus the two guards (register
// over-limit, complete exceeding source remainder). Skipped when
// LOAN_SMOKE_DSN unset.
//
// The posting half (Reserve/Post/Release) is proven by the finance
// two_phase_smoke; Zeebe routing is exercised by the CRM v2 flow.
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
		ContractDate: "2026-09-08",
		MaturityDate: "2027-09-08",
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
		DisburseDate:  "2026-09-08",
		DisburseAmt:   0,
		LoanTerm:      12,
		TermUnit:      "MONTH",
		MaturityDate:  "2027-09-08",
		Status:        "PENDING",
		CreatedBy:     "smoke",
	}); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}

	fake := &fakeWorkflow{}
	svc := NewDisbursementService(repo, fake)
	journal := func() string {
		return fmt.Sprintf("0197c0de-0000-7000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000) // journal entry UUID shape
	}
	agreementPending := func() int64 {
		var pending int64
		if err := db.QueryRow(`SELECT pending_disburse_amt_minor FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`,
			tenantID, agreementCode).Scan(&pending); err != nil {
			t.Fatalf("reload agreement pending: %v", err)
		}
		return pending
	}
	contractStatus := func() string {
		var status string
		if err := db.QueryRow(`SELECT status FROM lnm_contracts WHERE id = $1`, contract.ID).Scan(&status); err != nil {
			t.Fatalf("reload contract: %v", err)
		}
		return status
	}

	// ── REGISTER flow: create → submit → check → detail → settle ──
	register, err := svc.Create(ctx, tenantID, "smoke", &domain.Disbursement{
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		DisburseDate:     "2026-09-08",
		DisburseAmtMinor: 500_000_000,
		CurrencyCode:     "VND",
		FlowType:         domain.FlowRegister,
	})
	if err != nil {
		t.Fatalf("register create: %v", err)
	}
	if register.Status != domain.DisbursementDraft || register.FlowType != domain.FlowRegister {
		t.Fatalf("register draft = %+v", register)
	}
	registerSubmitted, err := svc.Submit(ctx, tenantID, "smoke", register.ID)
	if err != nil {
		t.Fatalf("register submit: %v", err)
	}
	if registerSubmitted.Status != domain.DisbursementSubmitted {
		t.Fatalf("register status after submit = %q", registerSubmitted.Status)
	}
	if len(fake.created) != 1 || fake.created[0].CaseType != RegisterCaseType {
		t.Fatalf("register case create mismatch: %+v", fake.created)
	}
	if len(fake.submitted) != 1 {
		t.Fatalf("register case submit missing")
	}
	if ok, message, err := svc.Check(ctx, tenantID, register.ID); err != nil || !ok {
		t.Fatalf("register check: ok=%v message=%q err=%v", ok, message, err)
	}
	detail, err := svc.PostingDetail(ctx, tenantID, register.ID)
	if err != nil {
		t.Fatalf("register posting detail: %v", err)
	}
	if detail.GetFlowType() != domain.FlowRegister || detail.GetDisburseAmtMinor() != register.DisburseAmtMinor {
		t.Fatalf("register detail mismatch: %+v", detail)
	}
	if detail.GetDebtGroupCode() == "" || detail.GetCustomerCode() == "" {
		t.Fatalf("register detail analytics incomplete: %+v", detail)
	}
	registerJournal := journal()
	if err := svc.SettleRegister(ctx, tenantID, register.ID, registerJournal, "smoke"); err != nil {
		t.Fatalf("register settle: %v", err)
	}
	registerFinal, err := svc.Get(ctx, tenantID, register.ID)
	if err != nil {
		t.Fatalf("register reload: %v", err)
	}
	if registerFinal.Status != domain.DisbursementPosted || registerFinal.JournalEntryID == nil || *registerFinal.JournalEntryID != registerJournal {
		t.Fatalf("register final = %+v, want POSTED with journal %s", registerFinal, registerJournal)
	}
	registerAgreement, err := repo.GetAgreementByCode(ctx, tenantID, agreementCode)
	if err != nil {
		t.Fatalf("reload agreement: %v", err)
	}
	if registerAgreement.OutstandingAmt != register.DisburseAmtMinor {
		t.Fatalf("outstanding = %d, want %d", registerAgreement.OutstandingAmt, register.DisburseAmtMinor)
	}
	if pending := agreementPending(); pending != register.DisburseAmtMinor {
		t.Fatalf("pending after register settle = %d, want %d", pending, register.DisburseAmtMinor)
	}
	if registerAgreement.Status != "ACTIVE" {
		t.Fatalf("agreement status = %q, want ACTIVE after register settle", registerAgreement.Status)
	}
	// Register settle must NOT flip the contract — that is the COMPLETE leg's job.
	if status := contractStatus(); status != domain.ContractDraft {
		t.Fatalf("contract status after register settle = %q, want DRAFT", status)
	}

	// ── COMPLETE flow: create(source=register) → submit → settle ──
	complete, err := svc.Create(ctx, tenantID, "smoke", &domain.Disbursement{
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		DisburseDate:     "2026-09-08",
		DisburseAmtMinor: 300_000_000,
		CurrencyCode:     "VND",
		FlowType:         domain.FlowComplete,
		SourceRegisterID: register.ID,
	})
	if err != nil {
		t.Fatalf("complete create: %v", err)
	}
	if complete.FlowType != domain.FlowComplete || complete.SourceRegisterID != register.ID {
		t.Fatalf("complete draft = %+v", complete)
	}
	completeSubmitted, err := svc.Submit(ctx, tenantID, "smoke", complete.ID)
	if err != nil {
		t.Fatalf("complete submit: %v", err)
	}
	if completeSubmitted.Status != domain.DisbursementSubmitted {
		t.Fatalf("complete status after submit = %q", completeSubmitted.Status)
	}
	if len(fake.created) != 2 || fake.created[1].CaseType != CompleteCaseType {
		t.Fatalf("complete case create mismatch: %+v", fake.created)
	}
	if ok, message, err := svc.Check(ctx, tenantID, complete.ID); err != nil || !ok {
		t.Fatalf("complete check: ok=%v message=%q err=%v", ok, message, err)
	}
	if err := svc.SettleComplete(ctx, tenantID, complete.ID, journal(), "smoke"); err != nil {
		t.Fatalf("complete settle: %v", err)
	}
	completeFinal, err := svc.Get(ctx, tenantID, complete.ID)
	if err != nil {
		t.Fatalf("complete reload: %v", err)
	}
	if completeFinal.Status != domain.DisbursementPosted {
		t.Fatalf("complete final status = %q, want POSTED", completeFinal.Status)
	}
	completeAgreement, err := repo.GetAgreementByCode(ctx, tenantID, agreementCode)
	if err != nil {
		t.Fatalf("reload agreement: %v", err)
	}
	if completeAgreement.OutstandingAmt != register.DisburseAmtMinor {
		t.Fatalf("outstanding after complete settle = %d, want unchanged %d", completeAgreement.OutstandingAmt, register.DisburseAmtMinor)
	}
	wantPending := register.DisburseAmtMinor - complete.DisburseAmtMinor
	if pending := agreementPending(); pending != wantPending {
		t.Fatalf("pending after complete settle = %d, want %d", pending, wantPending)
	}
	// First COMPLETE for the contract flips it ACTIVE (EPAS first-disbursement).
	if status := contractStatus(); status != domain.ContractActive {
		t.Fatalf("contract status after complete settle = %q, want ACTIVE", status)
	}

	// ── Guards ──
	// Register over contract limit: headroom is loan 1B - outstanding 500M;
	// 600M does not fit.
	if _, err := svc.Create(ctx, tenantID, "smoke", &domain.Disbursement{
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		DisburseDate:     "2026-09-08",
		DisburseAmtMinor: 600_000_000,
		CurrencyCode:     "VND",
		FlowType:         domain.FlowRegister,
	}); err == nil || !strings.Contains(err.Error(), "exceeds contract headroom") {
		t.Fatalf("register over-limit guard: err=%v", err)
	}
	// Complete exceeding source remainder: 300M of the 500M register is
	// already completed, another 300M does not fit.
	if _, err := svc.Create(ctx, tenantID, "smoke", &domain.Disbursement{
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		DisburseDate:     "2026-09-08",
		DisburseAmtMinor: 300_000_000,
		CurrencyCode:     "VND",
		FlowType:         domain.FlowComplete,
		SourceRegisterID: register.ID,
	}); err == nil || !strings.Contains(err.Error(), "exceeds source remainder") {
		t.Fatalf("complete remainder guard: err=%v", err)
	}
	// COMPLETE without a source register is rejected outright.
	if _, err := svc.Create(ctx, tenantID, "smoke", &domain.Disbursement{
		ContractCode:     contractCode,
		AgreementCode:    agreementCode,
		DisburseDate:     "2026-09-08",
		DisburseAmtMinor: 1_000_000,
		CurrencyCode:     "VND",
		FlowType:         domain.FlowComplete,
	}); err == nil || !strings.Contains(err.Error(), "source_register_id") {
		t.Fatalf("complete missing-source guard: err=%v", err)
	}
}
