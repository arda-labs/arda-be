package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

type batchFinanceFake struct {
	mu               sync.Mutex
	failAgreement    string
	failuresLeft     int
	callsByAgreement map[string]int
	acceptedByKey    map[string]string
	nextEntry        int
}

func newBatchFinanceFake(failAgreement string) *batchFinanceFake {
	return &batchFinanceFake{
		failAgreement:    failAgreement,
		failuresLeft:     1,
		callsByAgreement: make(map[string]int),
		acceptedByKey:    make(map[string]string),
	}
}

func (f *batchFinanceFake) Post(_ context.Context, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agreement := req.GetBusinessReference().GetDocumentCode()
	f.callsByAgreement[agreement]++
	entryID, ok := f.acceptedByKey[req.GetIdempotencyKey()]
	if !ok {
		f.nextEntry++
		entryID = fmt.Sprintf("00000000-0000-7000-8000-%012d", f.nextEntry)
		f.acceptedByKey[req.GetIdempotencyKey()] = entryID
	}
	if agreement == f.failAgreement && f.failuresLeft > 0 {
		f.failuresLeft--
		return nil, errors.New("PubAck lost after finance accepted posting")
	}
	return &financev1.PostingResponse{JournalEntryId: entryID}, nil
}

func (f *batchFinanceFake) ListPostingRules(context.Context, string) ([]*financev1.PostingRule, error) {
	return nil, nil
}

func (f *batchFinanceFake) uniquePosts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.acceptedByKey)
}

func (f *batchFinanceFake) calls(agreement string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callsByAgreement[agreement]
}

func TestAccrualBatchContinuesAndRetriesPendingWithoutDuplicatePost(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	repo := repository.NewLoanRepository(db)
	codes := seedAccrualBatchAgreements(t, db, repo)
	fake := newBatchFinanceFake(codes[1])
	svc := NewAccrualService(repo, db, nil)
	svc.finance = fake
	ctx := context.Background()

	first, err := svc.RunDaily(ctx, testTenantID, "2026-09-07", "batch-test")
	if err != nil {
		t.Fatalf("first accrual batch: %v", err)
	}
	if first.Processed != 2 || len(first.FailedDetail) != 1 {
		t.Fatalf("first result processed/failed = %d/%d, want 2/1", first.Processed, len(first.FailedDetail))
	}
	assertBatchStatuses(t, db, "lnm_accruals", codes, "to_date", "2026-09-07", 2, 1)

	second, err := svc.RunDaily(ctx, testTenantID, "2026-09-07", "batch-test")
	if err != nil {
		t.Fatalf("accrual retry batch: %v", err)
	}
	if second.Processed != 1 || len(second.FailedDetail) != 0 {
		t.Fatalf("retry result processed/failed = %d/%d, want 1/0", second.Processed, len(second.FailedDetail))
	}
	assertBatchStatuses(t, db, "lnm_accruals", codes, "to_date", "2026-09-07", 3, 0)
	assertNoDuplicateFinancePosts(t, fake, codes)
}

func TestProvisionBatchContinuesAndRetriesPendingWithoutDuplicatePost(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	repo := repository.NewLoanRepository(db)
	codes := seedAccrualBatchAgreements(t, db, repo)
	fake := newBatchFinanceFake(codes[1])
	svc := NewProvisionService(repo, db, nil)
	svc.finance = fake
	ctx := context.Background()

	first, err := svc.Run(ctx, testTenantID, "2026-09-07", "batch-test")
	if err != nil {
		t.Fatalf("first provision batch: %v", err)
	}
	if first.Processed != 2 || len(first.FailedAgmt) != 1 {
		t.Fatalf("first result processed/failed = %d/%d, want 2/1", first.Processed, len(first.FailedAgmt))
	}
	assertBatchStatuses(t, db, "lnm_provisions", codes, "provision_date", "2026-09-07", 2, 1)

	second, err := svc.Run(ctx, testTenantID, "2026-09-07", "batch-test")
	if err != nil {
		t.Fatalf("provision retry batch: %v", err)
	}
	if second.Processed != 1 || len(second.FailedAgmt) != 0 {
		t.Fatalf("retry result processed/failed = %d/%d, want 1/0", second.Processed, len(second.FailedAgmt))
	}
	assertBatchStatuses(t, db, "lnm_provisions", codes, "provision_date", "2026-09-07", 3, 0)
	assertNoDuplicateFinancePosts(t, fake, codes)
}

func TestProvisionBatchStoresSignedDeltaForReversal(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	repo := repository.NewLoanRepository(db)
	codes := seedAccrualBatchAgreements(t, db, repo)
	fake := newBatchFinanceFake("")
	svc := NewProvisionService(repo, db, nil)
	svc.finance = fake
	ctx := context.Background()
	if _, err := svc.Run(ctx, testTenantID, "2026-09-07", "batch-test"); err != nil {
		t.Fatalf("initial provision batch: %v", err)
	}
	if _, err := db.Exec(`UPDATE lnm_agreements SET outstanding_amt_minor = 400000000 WHERE tenant_id = $1 AND agreement_code = $2`, testTenantID, codes[0]); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(ctx, testTenantID, "2026-09-08", "batch-test")
	if err != nil {
		t.Fatalf("provision reversal batch: %v", err)
	}
	if result.Processed != 1 || result.NetMinor != -20000000 {
		t.Fatalf("reversal result processed/net = %d/%d, want 1/-20000000", result.Processed, result.NetMinor)
	}
	var delta, accumulated int64
	if err := db.QueryRow(`SELECT delta_minor FROM lnm_provisions WHERE tenant_id = $1 AND agreement_code = $2 AND provision_date = '2026-09-08'`, testTenantID, codes[0]).Scan(&delta); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT SUM(delta_minor) FROM lnm_provisions WHERE tenant_id = $1 AND agreement_code = $2`, testTenantID, codes[0]).Scan(&accumulated); err != nil {
		t.Fatal(err)
	}
	if delta != -20000000 || accumulated != 80000000 {
		t.Fatalf("reversal delta/accumulated = %d/%d, want -20000000/80000000", delta, accumulated)
	}
}

const testTenantID = "00000000-0000-0000-0000-000000000010"

func seedAccrualBatchAgreements(t *testing.T, db *sql.DB, repo *repository.LoanRepository) []string {
	t.Helper()
	run := time.Now().UTC().Format("20060102T150405.000000000")
	codes := []string{"A", "B", "C"}
	for i := range codes {
		code := "BATCH" + codes[i] + "-" + run
		codes[i] = code
		if _, err := repo.CreateAgreement(context.Background(), &domain.Agreement{
			ID:            repository.NewID("agr"),
			TenantID:      testTenantID,
			ContractCode:  "BATCH-CONTRACT-" + codes[i],
			AgreementCode: code,
			DisburseDate:  "2026-09-01",
			MaturityDate:  "2027-09-01",
			LoanTerm:      12,
			TermUnit:      "MONTH",
			Status:        "ACTIVE",
			CreatedBy:     "batch-test",
		}); err != nil {
			t.Fatalf("seed agreement %s: %v", code, err)
		}
		if _, err := db.Exec(`UPDATE lnm_agreements SET outstanding_amt_minor = 500000000, interest_rate = 8.5, debt_group_code = 'GROUP_3' WHERE tenant_id = $1 AND agreement_code = $2`, testTenantID, code); err != nil {
			t.Fatalf("set agreement amounts for %s: %v", code, err)
		}
	}
	return codes
}

func assertBatchStatuses(t *testing.T, db *sql.DB, table string, codes []string, dateColumn, date string, wantPosted, wantPending int) {
	t.Helper()
	query := fmt.Sprintf(`SELECT status, count(*) FROM %s WHERE tenant_id = $1 AND agreement_code = ANY($2) AND %s = $3::date GROUP BY status`, table, dateColumn)
	rows, err := db.Query(query, testTenantID, codes, date)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	posted, pending := 0, 0
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatal(err)
		}
		switch status {
		case "POSTED":
			posted = count
		case "PENDING":
			pending = count
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if posted != wantPosted || pending != wantPending {
		t.Fatalf("%s posted/pending = %d/%d, want %d/%d", table, posted, pending, wantPosted, wantPending)
	}
}

func assertNoDuplicateFinancePosts(t *testing.T, fake *batchFinanceFake, codes []string) {
	t.Helper()
	if got := fake.uniquePosts(); got != 3 {
		t.Fatalf("unique finance posts = %d, want 3", got)
	}
	for i, code := range codes {
		wantCalls := 1
		if i == 1 {
			wantCalls = 2
		}
		if got := fake.calls(code); got != wantCalls {
			t.Fatalf("finance calls for %s = %d, want %d", code, got, wantCalls)
		}
	}
}
