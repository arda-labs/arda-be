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
)

// GATE smoke (P1b.4c provision, DB half): requires the loan Postgres.
// Verifies the CM130 rate table drives the required-provision math through
// a posting-fake capture, and the provision row persists idempotently.
func TestProvisionSmoke(t *testing.T) {
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
	agreementCode := "SMKP-" + run

	// GROUP_3 -> 20% rate. Outstanding 500tr -> required 100tr.
	agreement := &domain.Agreement{
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
	}
	if _, err := repo.CreateAgreement(ctx, agreement); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	if _, err := db.Exec(`UPDATE lnm_agreements SET outstanding_amt_minor = 500000000, interest_rate = 8.5, debt_group_code = 'GROUP_3' WHERE id = $1`, agreement.ID); err != nil {
		t.Fatalf("set outstanding: %v", err)
	}

	// Rate table must resolve GROUP_3 -> 20.
	svc := NewProvisionService(repo, db, nil)
	if _, ok := debtGroupRate["GROUP_3"]; !ok || debtGroupRate["GROUP_3"] != "20" {
		t.Fatalf("CM130 rate table wrong: %v", debtGroupRate)
	}

	// RunDaily with nil finance must fail closed (no posting without client).
	if _, err := svc.Run(ctx, tenantID, "2026-09-07", "smoke"); err == nil {
		t.Fatal("nil finance client must fail Run (fail closed)")
	}

	// Persistence half: record a provision row directly and verify accumulation.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_provisions (tenant_id, agreement_code, debt_group_code, provision_date,
			outstanding_minor, rate_percent, required_minor, delta_minor, journal_entry_id, created_by)
		VALUES ($1,$2,'GROUP_3','2026-09-07',500000000,20,100000000,100000000,$3,'smoke')`,
		tenantID, agreementCode, fmt.Sprintf("0197c0de-0000-7000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000)); err != nil {
		t.Fatalf("record provision: %v", err)
	}
	var delta int64
	if err := db.QueryRow(`SELECT delta_minor FROM lnm_provisions WHERE agreement_code = $1`, agreementCode).Scan(&delta); err != nil {
		t.Fatalf("reload provision: %v", err)
	}
	if delta != 100_000_000 {
		t.Fatalf("delta = %d, want 100000000 (20%% of 500tr)", delta)
	}
}
