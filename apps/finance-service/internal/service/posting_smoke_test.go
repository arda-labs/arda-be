package service

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// GATE smoke (P1a): requires a disposable Postgres. Runs finance migrations,
// seeds COA + class maps + period, then drives PostTransaction → idempotent
// replay → outbox → ReverseTransaction. Skipped when FINANCE_SMOKE_DSN unset.
//
//	local: docker run -d --name arda-finance-smoke -e POSTGRES_PASSWORD=smoke \
//	         -p 55432:5432 postgres:16-alpine
//	       FINANCE_SMOKE_DSN=postgres://postgres:smoke@localhost:55432/postgres \
//	         go test ./internal/service -run TestPostingSmoke -v
func TestPostingSmoke(t *testing.T) {
	dsn := os.Getenv("FINANCE_SMOKE_DSN")
	if dsn == "" {
		t.Skip("FINANCE_SMOKE_DSN not set")
	}
	const tenantID = "00000000-0000-0000-0000-000000000010"

	db, err := sql.Open("pgx/v5", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := migration.Run(db, "postgres"); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	// Seed: COA version + accounts + class maps + open period.
	for _, stmt := range []string{
		`INSERT INTO fin_coa_versions (tenant_id, code, name, effective_date)
		 VALUES ('` + tenantID + `','V1','COA 2026','2026-01-01')
		 ON CONFLICT (tenant_id, code) DO NOTHING`,
		`INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
		 VALUES
		   ('` + tenantID + `','V1','1311','Cho vay khách hàng','ASSET','D'),
		   ('` + tenantID + `','V1','1131','Tiền gửi ngân hàng','ASSET','D'),
		   ('` + tenantID + `','V1','5111','Doanh thu lãi cho vay','INCOME','C')
		 ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING`,
		`INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
		 VALUES
		   ('` + tenantID + `','LNM_LOAN_PRINCIPAL','V1','1311','2026-01-01'),
		   ('` + tenantID + `','FUND_DISBURSEMENT_IN_TRANSIT','V1','1131','2026-01-01'),
		   ('` + tenantID + `','LNM_INTEREST_INCOME','V1','5111','2026-01-01')
		 ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING`,
		`INSERT INTO fin_periods (tenant_id, period_code, start_date, end_date, status)
		 VALUES ('` + tenantID + `','2026-09','2026-09-01','2026-09-30','OPEN')
		 ON CONFLICT (tenant_id, period_code) DO NOTHING`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}

	ctx := context.Background()
	svc := NewPostingService(repository.NewPostingRepository(db), db)

	req := &financev1.PostingRequest{
		IdempotencyKey: "smoke-001",
		AccountingDate: "2026-09-07",
		CurrencyCode:   "VND",
		Description:    "Giải ngân HD-001",
		BusinessReference: &financev1.BusinessReference{
			Domain:       "lnm",
			DocumentType: "LNM_DISBURSEMENT",
			DocumentCode: "DISB-1",
			CaseId:       "22222222-2222-2222-2222-222222222222",
		},
		Lines: []*financev1.PostingLine{
			{
				LineNo: 1, Direction: "DEBIT", AmountMinor: 500_000_000,
				Analytics: &financev1.Analytics{
					AccClassification: "LNM_LOAN_PRINCIPAL",
					DebtGroupCode:     "05",
					ContractCode:      "HD-001",
					OrgUnitCode:       "HO",
				},
			},
			{
				LineNo: 2, Direction: "CREDIT", AmountMinor: 500_000_000,
				Analytics: &financev1.Analytics{
					AccClassification: "FUND_DISBURSEMENT_IN_TRANSIT",
					OrgUnitCode:       "HO",
				},
			},
		},
	}

	// 1. Validate resolves both lines.
	validation, err := svc.ValidatePosting(ctx, tenantID, req)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !validation.GetValid() {
		t.Fatalf("validation failed: %v / %v", validation.GetGlobalErrors(), validation.GetLines())
	}
	if validation.GetLines()[0].GetAccountCode() != "1311" {
		t.Fatalf("line 1 resolved to %q, want 1311", validation.GetLines()[0].GetAccountCode())
	}
	if validation.GetCoaVersionId() != "V1" {
		t.Fatalf("coa version = %q, want V1", validation.GetCoaVersionId())
	}

	// 2. Unknown dimension key is rejected (registry whitelist, §8.2).
	reqWithBadDim, _ := cloneRequest(req)
	reqWithBadDim.Lines[0].Analytics.Dimensions = map[string]string{"hacker_key": "1"}
	if v, err := svc.ValidatePosting(ctx, tenantID, reqWithBadDim); err == nil && v.GetValid() {
		t.Fatal("unknown dimension must invalidate the request")
	}

	// 3. Unbalanced request is rejected globally.
	unbalanced, _ := cloneRequest(req)
	unbalanced.Lines[1].AmountMinor = 400_000_000
	if v, err := svc.ValidatePosting(ctx, tenantID, unbalanced); err == nil {
		if v.GetValid() {
			t.Fatal("unbalanced entry must not validate")
		}
		found := false
		for _, g := range v.GetGlobalErrors() {
			if len(g) >= 10 && g[:10] == "UNBALANCED" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected UNBALANCED error, got %v", v.GetGlobalErrors())
		}
	}

	// 4. Post succeeds.
	resp, err := svc.PostTransaction(ctx, tenantID, req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.GetEntryNo() <= 0 {
		t.Fatalf("entry_no must be positive, got %d", resp.GetEntryNo())
	}

	// 5. Idempotent replay returns the same entry.
	replay, err := svc.PostTransaction(ctx, tenantID, req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.GetReplayed() || replay.GetJournalEntryId() != resp.GetJournalEntryId() {
		t.Fatalf("replay mismatch: %+v vs %+v", replay, resp)
	}

	// 6. Outbox row enqueued.
	var outboxCount int
	if err := db.QueryRow(`SELECT count(*) FROM fin_outbox WHERE tenant_id = $1 AND published_at IS NULL`, tenantID).Scan(&outboxCount); err != nil {
		t.Fatalf("outbox check: %v", err)
	}
	if outboxCount == 0 {
		t.Fatal("expected pending outbox row after posting")
	}

	// 7. Reversal flips lines and marks original reversed.
	rev, err := svc.ReverseTransaction(ctx, &financev1.ReverseRequest{
		TenantId:       tenantID,
		JournalEntryId: resp.GetJournalEntryId(),
		Reason:         "smoke",
		IdempotencyKey: "smoke-001-rev",
		Actor:          "smoke",
		AccountingDate: "2026-09-07",
	})
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if rev.GetJournalEntryId() == resp.GetJournalEntryId() {
		t.Fatal("reversal must be a new entry")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM fin_journal_entries WHERE id = $1`, resp.GetJournalEntryId()).Scan(&status); err != nil {
		t.Fatalf("reload original: %v", err)
	}
	if status != "REVERSED" {
		t.Fatalf("original status = %q, want REVERSED", status)
	}
	slog.Info("posting smoke complete", "entry", resp.GetJournalEntryId(), "reversal", rev.GetJournalEntryId(), "at", time.Now().Format(time.RFC3339))
}

// cloneRequest deep-copies what the smoke mutates.
func cloneRequest(req *financev1.PostingRequest) (*financev1.PostingRequest, error) {
	clone := &financev1.PostingRequest{
		IdempotencyKey:  req.GetIdempotencyKey(),
		AccountingDate:  req.GetAccountingDate(),
		CurrencyCode:    req.GetCurrencyCode(),
		Description:     req.GetDescription(),
		BusinessReference: req.GetBusinessReference(),
	}
	for _, l := range req.GetLines() {
		clone.Lines = append(clone.Lines, &financev1.PostingLine{
			LineNo:      l.GetLineNo(),
			Direction:   l.GetDirection(),
			AmountMinor: l.GetAmountMinor(),
			Analytics: &financev1.Analytics{
				AccClassification: l.GetAnalytics().GetAccClassification(),
				DebtGroupCode:     l.GetAnalytics().GetDebtGroupCode(),
				OrgUnitCode:       l.GetAnalytics().GetOrgUnitCode(),
				ContractCode:      l.GetAnalytics().GetContractCode(),
			},
		})
	}
	return clone, nil
}
