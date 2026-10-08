package service

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

const collectionSettleTenant = "00000000-0000-0000-0000-000000000010"

func newCollectionSettleFixture(t *testing.T, status string) (*sql.DB, *CollectionService, string, string) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	t.Cleanup(func() { _ = db.Close() })

	repo := repository.NewLoanRepository(db)
	ctx := context.Background()
	runID := repository.NewID("settle")
	agreementCode := "AGR-" + runID
	contractCode := "CTR-" + runID
	if _, err := db.ExecContext(ctx, `
		INSERT INTO lnm_agreements
			(id, tenant_id, contract_code, agreement_code, outstanding_amt_minor,
			 coln_principal_amt_minor, coln_interest_amt_minor, status, created_by)
		VALUES ($1,$2,$3,$4,1000,0,0,'ACTIVE','settle-test')`,
		repository.NewID("agr"), collectionSettleTenant, contractCode, agreementCode); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	var collectionID string
	err := db.QueryRowContext(ctx, `
		INSERT INTO lnm_collections
			(tenant_id, contract_code, agreement_code, collection_date, principal_minor,
			 interest_minor, currency_code, status, created_by)
		VALUES ($1,$2,$3,'2026-09-07',300,50,'VND',$4,'settle-test')
		RETURNING id::text`, collectionSettleTenant, contractCode, agreementCode, status).Scan(&collectionID)
	if err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	return db, NewCollectionService(repo, nil), agreementCode, collectionID
}

func assertCollectionSettleTotals(t *testing.T, db *sql.DB, svc *CollectionService, agreementCode, collectionID string, wantStatus string) {
	t.Helper()
	var outstanding, principal, interest int64
	err := db.QueryRowContext(context.Background(), `
		SELECT outstanding_amt_minor, coln_principal_amt_minor, coln_interest_amt_minor
		FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`,
		collectionSettleTenant, agreementCode).Scan(&outstanding, &principal, &interest)
	if err != nil {
		t.Fatalf("reload agreement totals: %v", err)
	}
	if outstanding != 700 || principal != 300 || interest != 50 {
		t.Fatalf("agreement totals = outstanding %d, collected principal %d, interest %d; want 700/300/50",
			outstanding, principal, interest)
	}
	collection, err := svc.Get(context.Background(), collectionSettleTenant, collectionID)
	if err != nil {
		t.Fatalf("reload collection: %v", err)
	}
	if collection.Status != wantStatus {
		t.Fatalf("collection status = %q, want %q", collection.Status, wantStatus)
	}
}

func TestCollectionSettle_Idempotent(t *testing.T) {
	db, svc, agreementCode, collectionID := newCollectionSettleFixture(t, domain.CollectionApproved)
	firstJournal := "0197c0de-0000-7000-8000-000000000001"
	secondJournal := "0197c0de-0000-7000-8000-000000000002"
	ctx := context.Background()

	if err := svc.Settle(ctx, collectionSettleTenant, collectionID, firstJournal, "settle-test"); err != nil {
		t.Fatalf("first settle: %v", err)
	}
	if err := svc.Settle(ctx, collectionSettleTenant, collectionID, secondJournal, "settle-replay"); err != nil {
		t.Fatalf("replayed settle: %v", err)
	}
	assertCollectionSettleTotals(t, db, svc, agreementCode, collectionID, domain.CollectionPosted)
	collection, err := svc.Get(ctx, collectionSettleTenant, collectionID)
	if err != nil {
		t.Fatalf("reload collection: %v", err)
	}
	if collection.JournalEntryID == nil || *collection.JournalEntryID != firstJournal {
		t.Fatalf("journal entry after replay = %v, want original %q", collection.JournalEntryID, firstJournal)
	}
}

func TestCollectionSettle_RejectsNonApproved(t *testing.T) {
	for _, status := range []string{domain.CollectionDraft, domain.CollectionPosted, domain.CollectionRejected} {
		t.Run(status, func(t *testing.T) {
			db, svc, agreementCode, collectionID := newCollectionSettleFixture(t, status)
			err := svc.Settle(context.Background(), collectionSettleTenant, collectionID,
				"0197c0de-0000-7000-8000-000000000003", "settle-test")
			if err == nil {
				t.Fatalf("Settle from %s should fail", status)
			}
			var outstanding, principal, interest int64
			err = db.QueryRowContext(context.Background(), `
				SELECT outstanding_amt_minor, coln_principal_amt_minor, coln_interest_amt_minor
				FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`,
				collectionSettleTenant, agreementCode).Scan(&outstanding, &principal, &interest)
			if err != nil {
				t.Fatalf("reload agreement totals: %v", err)
			}
			if outstanding != 1000 || principal != 0 || interest != 0 {
				t.Fatalf("non-approved settle changed agreement: outstanding %d, collected principal %d, interest %d",
					outstanding, principal, interest)
			}
			collection, err := svc.Get(context.Background(), collectionSettleTenant, collectionID)
			if err != nil {
				t.Fatalf("reload collection: %v", err)
			}
			if collection.Status != status || collection.JournalEntryID != nil {
				t.Fatalf("non-approved collection changed: status=%q journal=%v", collection.Status, collection.JournalEntryID)
			}
		})
	}
}

func TestCollectionSettle_Concurrent(t *testing.T) {
	db, svc, agreementCode, collectionID := newCollectionSettleFixture(t, domain.CollectionApproved)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 1; i <= 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			journalID := fmt.Sprintf("0197c0de-0000-7000-8000-%012d", i)
			errs <- svc.Settle(context.Background(), collectionSettleTenant, collectionID, journalID, "settle-race")
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent settle: %v", err)
		}
	}
	assertCollectionSettleTotals(t, db, svc, agreementCode, collectionID, domain.CollectionPosted)
}
