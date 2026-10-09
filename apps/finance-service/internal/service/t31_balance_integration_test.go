package service

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestBalanceSnapshotIncludesBackdatedPostingAfterRebuild(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-000000000031"
	const balanceDate = "2026-09-10"
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO fin_opening_balances
			(tenant_id, accounting_date, coa_version, account_code, currency_code, direction, amount_minor, created_by)
		VALUES ($1, '2026-09-01', 'V1', 'T31-BACKDATE', 'VND', 'DEBIT', 100, 't31-smoke')`, tenantID); err != nil {
		t.Fatalf("seed opening balance: %v", err)
	}
	if _, err := NewTrialBalanceDailyService(db).RebuildDaily(ctx, tenantID, balanceDate, "t31-smoke"); err != nil {
		t.Fatalf("build dated snapshot: %v", err)
	}
	var snapshotDebit int64
	if err := db.QueryRowContext(ctx, `
		SELECT close_debit_minor FROM fin_trial_balance_daily
		WHERE tenant_id = $1 AND business_date = $2::date AND account_code = 'T31-BACKDATE'
		  AND bal_type_code = $3`, tenantID, balanceDate, repository.BalanceTypeActual).Scan(&snapshotDebit); err != nil {
		t.Fatalf("read snapshot debit: %v", err)
	}
	if snapshotDebit != 100 {
		t.Fatalf("snapshot debit = %d, want opening 100", snapshotDebit)
	}

	var entryID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO fin_journal_entries
			(tenant_id, accounting_date, currency_code, status, description, business_domain, business_doc_type, created_by, posted_at)
		VALUES ($1, '2026-09-08', 'VND', 'POSTED', 'backdated after snapshot', 'fin', 'T31_SMOKE', 't31-smoke', now())
		RETURNING id::text`, tenantID).Scan(&entryID); err != nil {
		t.Fatalf("insert backdated entry: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO fin_journal_lines
			(tenant_id, entry_id, line_no, direction, bal_type_code, coa_version, account_code, account_name, amount_minor, currency_code)
		VALUES ($1, $2::uuid, 1, 'DEBIT', $3, 'V1', 'T31-BACKDATE', 'T31 smoke', 25, 'VND')`,
		tenantID, entryID, repository.BalanceTypeActual); err != nil {
		t.Fatalf("insert backdated line: %v", err)
	}

	got, err := NewStatementService(db).accountBalances(ctx, tenantID, balanceDate, "V1")
	if err != nil {
		t.Fatalf("read dated balance: %v", err)
	}
	if value := got["T31-BACKDATE"]; value.Debit != 125 || value.Credit != 0 {
		t.Fatalf("dated balance = Dr %d Cr %d, want Dr 125 Cr 0", value.Debit, value.Credit)
	}

	before, err := NewStatementService(db).accountBalances(ctx, tenantID, "2026-09-07", "V1")
	if err != nil {
		t.Fatalf("read balance before backdated posting date: %v", err)
	}
	if value := before["T31-BACKDATE"]; value.Debit != 100 {
		t.Fatalf("balance before entry date = %d, want opening 100", value.Debit)
	}
}

func TestOverlappingBalanceLocksUseDeterministicOrder(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-000000000032"
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	repo := repository.NewPostingRepository(db)
	keys := []repository.BalanceKey{
		{BalTypeCode: repository.BalanceTypeActual, CoaVersion: "V1", AccountCode: "T31-A", CurrencyCode: "VND"},
		{BalTypeCode: repository.BalanceTypeActual, CoaVersion: "V1", AccountCode: "T31-B", CurrencyCode: "VND"},
	}

	for attempt := 0; attempt < 50; attempt++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		errCh := make(chan error, 2)
		for worker := 0; worker < 2; worker++ {
			worker := worker
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					errCh <- err
					return
				}
				defer tx.Rollback()
				ordered := append([]repository.BalanceKey(nil), keys...)
				if worker == 1 {
					ordered[0], ordered[1] = ordered[1], ordered[0]
				}
				if _, err := repo.EnsureAndLockBalances(ctx, tx, tenantID, ordered); err != nil {
					errCh <- fmt.Errorf("attempt %d worker %d: %w", attempt, worker, err)
					return
				}
				if err := tx.Commit(); err != nil {
					errCh <- fmt.Errorf("attempt %d worker %d commit: %w", attempt, worker, err)
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Fatal(err)
		}
	}
}
