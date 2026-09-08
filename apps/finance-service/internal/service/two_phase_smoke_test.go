package service

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// GATE smoke (P1b v2 two-phase balance): requires a disposable Postgres.
// Drives the full lifecycle: Reserve (PENDING + hold, availability drops,
// actual untouched) → idempotent reserve replay → overdraft guard (second
// pending must fail) → Release (hold freed) → re-Reserve → Post (hold
// graduates to posted, availability preserved, outbox fires) → Reverse.
// Skipped when FINANCE_SMOKE_DSN unset.
//
//	FINANCE_SMOKE_DSN=<dsn> go test ./internal/service -run TestTwoPhaseBalanceSmoke -v
func TestTwoPhaseBalanceSmoke(t *testing.T) {
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

	for _, stmt := range []string{
		`INSERT INTO fin_coa_versions (tenant_id, code, name, effective_date)
		 VALUES ('` + tenantID + `','V1','COA 2026','2026-01-01')
		 ON CONFLICT (tenant_id, code) DO NOTHING`,
		`INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature)
		 VALUES
		   ('` + tenantID + `','V1','1311','Cho vay khách hàng','ASSET','D'),
		   ('` + tenantID + `','V1','1131','Tiền gửi ngân hàng','ASSET','D')
		 ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING`,
		`INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date)
		 VALUES
		   ('` + tenantID + `','LNM_LOAN_PRINCIPAL','V1','1311','2026-01-01'),
		   ('` + tenantID + `','FUND_DISBURSEMENT_IN_TRANSIT','V1','1131','2026-01-01')
		 ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING`,
		`INSERT INTO fin_periods (tenant_id, period_code, start_date, end_date, status)
		 VALUES ('` + tenantID + `','2026-09','2026-09-01','2026-09-30','OPEN')
		 ON CONFLICT (tenant_id, period_code) DO NOTHING`,
		// Opening balance: account 1131 holds 1_000_000 debit.
		`INSERT INTO fin_opening_balances (tenant_id, accounting_date, coa_version, account_code, currency_code, direction, amount_minor, description, created_by)
		 VALUES ('` + tenantID + `','2026-09-01','V1','1131','VND','DEBIT',1000000,'smoke opening','smoke')
		 ON CONFLICT (tenant_id, accounting_date, coa_version, account_code, currency_code) DO NOTHING`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}

	ctx := context.Background()
	svc := NewPostingService(repository.NewPostingRepository(db), db)

	// Re-run safety (live DB): drop artifacts of previous smoke runs —
	// entries, their outbox rows, and the seeded accounts' balance rows
	// (recreated from zero by the run itself; the opening balance supplies
	// the value). Safe because smoke doc codes are dedicated to the smokes.
	for _, stmt := range []string{
		`DELETE FROM fin_outbox WHERE tenant_id = '` + tenantID + `' AND aggregate_id IN (
			SELECT id FROM fin_journal_entries WHERE tenant_id = '` + tenantID + `'
			  AND business_doc_code IN ('DISB-2P','DISB-1'))`,
		`DELETE FROM fin_journal_lines WHERE tenant_id = '` + tenantID + `' AND entry_id IN (
			SELECT id FROM fin_journal_entries WHERE tenant_id = '` + tenantID + `'
			  AND business_doc_code IN ('DISB-2P','DISB-1'))`,
		`DELETE FROM fin_journal_entries WHERE tenant_id = '` + tenantID + `'
		   AND business_doc_code IN ('DISB-2P','DISB-1')`,
		`DELETE FROM fin_account_balances WHERE tenant_id = '` + tenantID + `'
		   AND coa_version = 'V1' AND account_code IN ('1131','1311') AND currency_code = 'VND'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("cleanup: %v\n%s", err, stmt)
		}
	}

	runKey := "smoke2p-" + time.Now().UTC().Format("20060102T150405.000000000")

	// disbursement proposal: DR 1311 (loan receivable) / CR 1131 (funds) 600.
	newReq := func(key string) *financev1.PostingRequest {
		return &financev1.PostingRequest{
			IdempotencyKey: runKey + key,
			AccountingDate: "2026-09-08",
			CurrencyCode:   "VND",
			Description:    "Giải ngân 2 pha",
			BusinessReference: &financev1.BusinessReference{
				Domain: "lnm", DocumentType: "LNM_DISB_REGISTER", DocumentCode: "DISB-2P",
			},
			Lines: []*financev1.PostingLine{
				{LineNo: 1, Direction: "DEBIT", AmountMinor: 600_000,
					Analytics: &financev1.Analytics{AccClassification: "LNM_LOAN_PRINCIPAL", OrgUnitCode: "HO"}},
				{LineNo: 2, Direction: "CREDIT", AmountMinor: 600_000,
					Analytics: &financev1.Analytics{AccClassification: "FUND_DISBURSEMENT_IN_TRANSIT", OrgUnitCode: "HO"}},
			},
		}
	}
	counters := func() (postedDr, postedCr, resvDr, resvCr int64) {
		err := db.QueryRow(`SELECT posted_debit_minor, posted_credit_minor, reserved_debit_minor, reserved_credit_minor
			FROM fin_account_balances WHERE tenant_id=$1 AND coa_version='V1' AND account_code='1131' AND currency_code='VND'`,
			tenantID).Scan(&postedDr, &postedCr, &resvDr, &resvCr)
		if err != nil {
			t.Fatalf("balance counters: %v", err)
		}
		return
	}

	// 1. Reserve → PENDING, hold on 1131 credit side, posted untouched.
	pending, err := svc.ReservePosting(ctx, tenantID, newReq(""))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if pending.GetStatus() != "PENDING" {
		t.Fatalf("reserve status = %q, want PENDING", pending.GetStatus())
	}
	if _, cr, _, resvCr := counters(); cr != 0 || resvCr != 600_000 {
		t.Fatalf("after reserve: posted_credit=%d reserved_credit=%d, want 0/600000", cr, resvCr)
	}

	// 2. Idempotent replay of the same reserve.
	replay, err := svc.ReservePosting(ctx, tenantID, newReq(""))
	if err != nil {
		t.Fatalf("reserve replay: %v", err)
	}
	if !replay.GetReplayed() || replay.GetJournalEntryId() != pending.GetJournalEntryId() {
		t.Fatalf("replay mismatch: %+v vs %+v", replay, pending)
	}

	// 3. Overdraft guard: second pending of 500 must fail (only 400 left).
	if _, err := svc.ReservePosting(ctx, tenantID, newReq("-second")); err == nil ||
		!strings.HasPrefix(err.Error(), "BAL_AVAILABLE_IS_NOT_ENOUGH") {
		t.Fatalf("second reserve must fail BAL_AVAILABLE_IS_NOT_ENOUGH, got %v", err)
	}

	// 4. Edited proposal re-reserves: same key, smaller amount → stale hold
	// released and replaced (the maker-edit loop).
	edited := newReq("")
	edited.Lines[0].AmountMinor, edited.Lines[1].AmountMinor = 400_000, 400_000
	editedResp, err := svc.ReservePosting(ctx, tenantID, edited)
	if err != nil {
		t.Fatalf("edited reserve: %v", err)
	}
	if editedResp.GetJournalEntryId() == pending.GetJournalEntryId() {
		t.Fatal("edited proposal must create a new entry (old one voided)")
	}
	if _, _, _, resvCr := counters(); resvCr != 400_000 {
		t.Fatalf("after rebuild: reserved_credit=%d, want 400000", resvCr)
	}

	// 5. Post graduates the hold to posted; availability preserved; outbox.
	posted, err := svc.PostTransaction(ctx, tenantID, edited)
	if err != nil {
		t.Fatalf("post pending: %v", err)
	}
	if posted.GetStatus() != "POSTED" || posted.GetReplayed() {
		t.Fatalf("post: %+v", posted)
	}
	postedDr, postedCr, _, resvCr := counters()
	if postedCr != 400_000 || resvCr != 0 {
		t.Fatalf("after post: posted_credit=%d reserved_credit=%d, want 400000/0", postedCr, resvCr)
	}
	if postedDr != 0 {
		t.Fatalf("1131 posted_debit must stay 0, got %d", postedDr)
	}
	var outboxCount int
	if err := db.QueryRow(`SELECT count(*) FROM fin_outbox WHERE tenant_id=$1 AND published_at IS NULL`, tenantID).Scan(&outboxCount); err != nil {
		t.Fatalf("outbox check: %v", err)
	}
	if outboxCount == 0 {
		t.Fatal("expected outbox row after posting pending entry")
	}

	// 6. Second flow: reserve then RELEASE — hold freed, entry VOID.
	// After step 5 the 400k hold graduated to posted; this new 600k hold is
	// the only reservation left (available = 1M opening − 400k posted − 600k
	// pending = 0 — fully committed, correct).
	rel := newReq("-rel")
	relPending, err := svc.ReservePosting(ctx, tenantID, rel)
	if err != nil {
		t.Fatalf("reserve for release: %v", err)
	}
	if _, _, _, resvCr := counters(); resvCr != 600_000 {
		t.Fatalf("pre-release hold = %d, want 600000", resvCr)
	}
	voided, err := svc.ReleasePosting(ctx, tenantID, &financev1.ReleaseRequest{
		JournalEntryId: relPending.GetJournalEntryId(), Reason: "smoke reject", Actor: "smoke"})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if voided.GetStatus() != "VOID" {
		t.Fatalf("release status = %q, want VOID", voided.GetStatus())
	}
	if _, _, _, resvCr := counters(); resvCr != 0 {
		t.Fatalf("after release reserved_credit=%d, want 0 (the 400k already graduated to posted)", resvCr)
	}
	// Released key is free again: a fresh reserve under the same key works.
	freshAgain, err := svc.ReservePosting(ctx, tenantID, rel)
	if err != nil {
		t.Fatalf("re-reserve after release: %v", err)
	}
	if freshAgain.GetJournalEntryId() == relPending.GetJournalEntryId() || freshAgain.GetStatus() != "PENDING" {
		t.Fatalf("re-reserve must create a fresh PENDING entry, got %+v", freshAgain)
	}

	// 7. Direct post (no hold) books straight to posted counters.
	direct := newReq("-direct")
	direct.Lines[0].Direction = "CREDIT"
	direct.Lines[1].Direction = "DEBIT"
	direct.BusinessReference.DocumentType = "LNM_DISB_COMPLETE"
	if _, err := svc.PostTransaction(ctx, tenantID, direct); err != nil {
		t.Fatalf("direct post: %v", err)
	}
	if dr, cr, _, _ := counters(); dr != 600_000 || cr != 400_000 {
		t.Fatalf("after direct post: posted_dr=%d posted_cr=%d, want 600000/400000", dr, cr)
	}

	// 8. Manual posting path: line carries account_code directly (FAC-native
	// single-entry shape), classification left empty; nature-B (off-balance
	// memo) account exempt from balance checks.
	memo := &financev1.PostingRequest{
		IdempotencyKey: runKey + "-memo",
		AccountingDate: "2026-09-08",
		CurrencyCode:   "VND",
		Description:    "Ngoại bảng nhập — memo",
		BusinessReference: &financev1.BusinessReference{
			Domain: "fin", DocumentType: "FIN_OFF_BALANCE", DocumentCode: "OFFB-1",
		},
		Lines: []*financev1.PostingLine{
			{LineNo: 1, Direction: "DEBIT", AmountMinor: 77_000,
				AccountCode: "1311", // nature D — but direction DEBIT is an inflow
				Analytics:   &financev1.Analytics{OrgUnitCode: "HO"}},
			{LineNo: 2, Direction: "CREDIT", AmountMinor: 77_000,
				AccountCode: "1311",
				Analytics:   &financev1.Analytics{OrgUnitCode: "HO"}},
		},
	}
	if v, err := svc.ValidatePosting(ctx, tenantID, memo); err != nil || !v.GetValid() {
		t.Fatalf("manual lines must validate: %v / %v", err, v.GetGlobalErrors())
	}
	if _, err := svc.PostTransaction(ctx, tenantID, memo); err != nil {
		t.Fatalf("manual direct post: %v", err)
	}

	slog.Info("two-phase balance smoke complete", "at", time.Now().Format(time.RFC3339))
}
