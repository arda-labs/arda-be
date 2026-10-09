package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"

	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// Pure guard tests for the maker revise (PUT /api/loan/contracts/{id}) —
// the DB-backed happy path lives in TestContractUpdateSmoke (ARDA_TEST_DSN
// gated), mirroring the disbursement guard/smoke split.

func TestContractEditableStatus(t *testing.T) {
	tests := []struct {
		status      string
		wantAllowed bool
	}{
		{domain.ContractDraft, true},
		{domain.ContractPendingApproval, true},
		{domain.ContractDisbursed, false},
		{domain.ContractRejected, true},
		{domain.ContractClosed, false},
		{"", false},
	}
	for _, tt := range tests {
		if got := contractEditableStatus(tt.status); got != tt.wantAllowed {
			t.Fatalf("contractEditableStatus(%q) = %v, want %v", tt.status, got, tt.wantAllowed)
		}
	}
}

func TestValidateContractUpdate(t *testing.T) {
	valid := &domain.Contract{LoanAmt: 500_000_000, InterestRate: 8.5, LoanTerm: 12}
	if err := validateContractUpdate(valid); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	if err := validateContractUpdate(&domain.Contract{LoanAmt: 0, InterestRate: 8.5, LoanTerm: 12}); err == nil {
		t.Fatal("expected loan_amt <= 0 to be rejected")
	}
	if err := validateContractUpdate(&domain.Contract{LoanAmt: 1, InterestRate: 0, LoanTerm: 12}); err == nil {
		t.Fatal("expected interest_rate <= 0 to be rejected")
	}
	if err := validateContractUpdate(&domain.Contract{LoanAmt: 1, InterestRate: 8.5, LoanTerm: 0}); err == nil {
		t.Fatal("expected loan_term <= 0 to be rejected")
	}
	if err := validateContractUpdate(&domain.Contract{LoanAmt: 1, InterestRate: 8.5, LoanTerm: 12, ContractDate: "07/09/2026"}); err == nil {
		t.Fatal("expected non-ISO contract_date to be rejected")
	}
	if err := validateContractUpdate(&domain.Contract{LoanAmt: 1, InterestRate: 8.5, LoanTerm: 12, MaturityDate: "2026-13-40"}); err == nil {
		t.Fatal("expected invalid maturity_date to be rejected")
	}
}

func TestValidateContractUpdateAcceptsOptionalDates(t *testing.T) {
	err := validateContractUpdate(&domain.Contract{LoanAmt: 1, InterestRate: 8.5, LoanTerm: 12,
		ContractDate: "2026-09-07", MaturityDate: "2027-09-07"})
	if err != nil {
		t.Fatalf("valid dates rejected: %v", err)
	}
}

// GATE smoke (maker revise, DB half): requires the loan Postgres. Seeds a
// DRAFT contract, updates the whitelist fields, then verifies the status
// guard freezes the contract once it leaves DRAFT/PENDING_APPROVAL/REJECTED.
func TestContractUpdateSmoke(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-000000000010"

	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewLoanRepository(db)
	run := time.Now().UTC().Format("20060102T150405.000000000")

	created, err := repo.CreateContract(ctx, &domain.Contract{
		ID:           repository.NewID("ctrt"),
		TenantID:     tenantID,
		ContractCode: "SMKU-" + run,
		CustomerCode: "KH-SMKU",
		ContractDate: "2026-09-07",
		MaturityDate: "2027-09-07",
		LoanTerm:     12,
		TermUnit:     "MONTH",
		LoanAmt:      500_000_000,
		InterestRate: 8.5,
		Status:       domain.ContractDraft,
		CreatedBy:    "smoke",
	})
	if err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	// Happy path: whitelist fields update, identity fields stay frozen.
	patch := domain.Contract{
		ContractNo:          "HD-2026-SMKU",
		LoanAmt:             750_000_000,
		InterestRate:        9.25,
		LoanTerm:            18,
		TermUnit:            "MONTH",
		ContractDate:        "2026-09-08",
		MaturityDate:        "2028-03-08",
		InterestScheduleDay: 5,
		PurposeCode:         "KINH_DOANH",
		EmployeeCode:        "CV-001",
	}
	updated, err := repo.UpdateContract(ctx, tenantID, created.ID, &patch)
	if err != nil {
		t.Fatalf("UpdateContract: %v", err)
	}
	if updated.LoanAmt != 750_000_000 || updated.InterestRate != 9.25 || updated.LoanTerm != 18 {
		t.Fatalf("whitelist fields not applied: %+v", updated)
	}
	if updated.ContractNo != "HD-2026-SMKU" || updated.PurposeCode != "KINH_DOANH" || updated.EmployeeCode != "CV-001" || updated.InterestScheduleDay != 5 {
		t.Fatalf("editable codes not applied: %+v", updated)
	}
	if updated.ContractDate != "2026-09-08" || updated.MaturityDate != "2028-03-08" {
		t.Fatalf("dates not applied: %+v", updated)
	}
	if updated.Status != domain.ContractDraft || updated.ContractCode != created.ContractCode || updated.CustomerCode != "KH-SMKU" {
		t.Fatalf("identity/status fields leaked into update: %+v", updated)
	}

	// Guard: once DISBURSED the contract is frozen.
	if err := repo.UpdateContractStatus(ctx, tenantID, created.ID, domain.ContractDraft, domain.ContractPendingApproval); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := repo.UpdateContractStatus(ctx, tenantID, created.ID, domain.ContractPendingApproval, domain.ContractApproved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := repo.UpdateContractStatus(ctx, tenantID, created.ID, domain.ContractApproved, domain.ContractDisbursed); err != nil {
		t.Fatalf("disburse: %v", err)
	}
	if _, err := repo.UpdateContract(ctx, tenantID, created.ID, &patch); !errors.Is(err, repository.ErrContractNotEditable) {
		 t.Fatalf("expected ErrContractNotEditable on DISBURSED contract, got %v", err)
	}

	// Not-found contract maps to ErrNotFound, not not-editable.
	if _, err := repo.UpdateContract(ctx, tenantID, "ctrt_missing", &patch); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing contract, got %v", err)
	}
}

// Service-level guard: the non-editable status is rejected with the
// contract_not_editable message before any repo write happens.
func TestUpdateContractGuardMessage(t *testing.T) {
	err := ardaerrors.New(ardaerrors.CodeInvalidInput, "contract_not_editable: only DRAFT, PENDING_APPROVAL, or REJECTED contracts can be revised")
	if err.Code != ardaerrors.CodeInvalidInput || err.Message[:21] != "contract_not_editable" {
		t.Fatalf("unexpected guard error shape: %+v", err)
	}
}
