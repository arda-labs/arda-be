package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// insertTBDailyEntry ghi một bút toán POSTED một dòng Nợ vào ngày hạch toán
// cho test số dư theo ngày (không đi qua luồng posting để cô lập RebuildDaily).
func insertTBDailyEntry(t *testing.T, db *sql.DB, tenantID, accountingDate, account string, amount int64) {
	t.Helper()
	ctx := context.Background()
	var entryID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO fin_journal_entries
			(tenant_id, accounting_date, currency_code, status, description, business_domain, business_doc_type, created_by, posted_at)
		VALUES ($1, $2::date, 'VND', 'POSTED', 'tb daily rebuild test', 'fin', 'TB_DAILY_TEST', 'tb-daily-test', clock_timestamp())
		RETURNING id::text`, tenantID, accountingDate).Scan(&entryID); err != nil {
		t.Fatalf("insert entry %s: %v", accountingDate, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO fin_journal_lines
			(tenant_id, entry_id, line_no, direction, bal_type_code, coa_version, account_code, account_name, amount_minor, currency_code)
		VALUES ($1, $2::uuid, 1, 'DEBIT', $3, 'V1', $4, 'TB daily test', $5, 'VND')`,
		tenantID, entryID, repository.BalanceTypeActual, account, amount); err != nil {
		t.Fatalf("insert line %s: %v", accountingDate, err)
	}
}

type tbDailyRow struct{ incrDebit, closeDebit int64 }

func readTBDaily(t *testing.T, db *sql.DB, tenantID, date, account string) tbDailyRow {
	t.Helper()
	var row tbDailyRow
	err := db.QueryRowContext(context.Background(), `
		SELECT incr_debit_minor, close_debit_minor FROM fin_trial_balance_daily
		WHERE tenant_id = $1 AND business_date = $2::date AND account_code = $3 AND bal_type_code = $4`,
		tenantID, date, account, repository.BalanceTypeActual).Scan(&row.incrDebit, &row.closeDebit)
	if err == sql.ErrNoRows {
		t.Fatalf("no daily row for %s on %s", account, date)
	}
	if err != nil {
		t.Fatalf("read daily row %s %s: %v", account, date, err)
	}
	return row
}

// AP-PER-02: bút toán lùi ngày vào một ngày đã dựng số dư phải làm ngày đó và
// mọi ngày sau được dựng lại ở lần COB kế tiếp.
func TestRebuildDailyRepairsBackdatedPosting(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-0000000000a2"
	const account = "TBD-BACKDATE"
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()
	daily := NewTrialBalanceDailyService(db)

	insertTBDailyEntry(t, db, tenantID, "2026-09-08", account, 100)
	for _, d := range []string{"2026-09-08", "2026-09-09"} {
		if _, err := daily.RebuildDaily(ctx, tenantID, d, "test"); err != nil {
			t.Fatalf("rebuild %s: %v", d, err)
		}
	}
	// Ghi lùi về 08/09 sau khi 08/09 và 09/09 đã dựng.
	insertTBDailyEntry(t, db, tenantID, "2026-09-08", account, 25)

	res, err := daily.RebuildDaily(ctx, tenantID, "2026-09-10", "test")
	if err != nil {
		t.Fatalf("rebuild 2026-09-10: %v", err)
	}
	if res.RebuiltFrom != "2026-09-08" {
		t.Fatalf("rebuilt_from = %q, want 2026-09-08 (first affected date)", res.RebuiltFrom)
	}
	if got := readTBDaily(t, db, tenantID, "2026-09-08", account); got.incrDebit != 125 || got.closeDebit != 125 {
		t.Fatalf("2026-09-08 = %+v, want incr 125 close 125", got)
	}
	for _, d := range []string{"2026-09-09", "2026-09-10"} {
		if got := readTBDaily(t, db, tenantID, d, account); got.closeDebit != 125 || got.incrDebit != 0 {
			t.Fatalf("%s = %+v, want incr 0 close 125", d, got)
		}
	}

	// Không có thay đổi mới: lần dựng sau không dựng lại lịch sử.
	res, err = daily.RebuildDaily(ctx, tenantID, "2026-09-10", "test")
	if err != nil {
		t.Fatalf("rerun 2026-09-10: %v", err)
	}
	if res.RebuiltFrom != "" {
		t.Fatalf("clean rerun rebuilt history from %q", res.RebuiltFrom)
	}
}

// AP-PER-02: COB chỉ chạy ngày làm việc. Bút toán vào thứ Bảy phải vào số dư
// của ngày dựng kế tiếp (thứ Hai), không bị bỏ khỏi mọi dòng số dư.
func TestRebuildDailyIncludesNonWorkingDayPosting(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-0000000000a3"
	const account = "TBD-WEEKEND"
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()
	daily := NewTrialBalanceDailyService(db)

	insertTBDailyEntry(t, db, tenantID, "2026-09-11", account, 100) // thứ Sáu
	if _, err := daily.RebuildDaily(ctx, tenantID, "2026-09-11", "test"); err != nil {
		t.Fatalf("rebuild Friday: %v", err)
	}
	insertTBDailyEntry(t, db, tenantID, "2026-09-12", account, 40) // thứ Bảy
	insertTBDailyEntry(t, db, tenantID, "2026-09-14", account, 5)  // thứ Hai
	if _, err := daily.RebuildDaily(ctx, tenantID, "2026-09-14", "test"); err != nil {
		t.Fatalf("rebuild Monday: %v", err)
	}
	if got := readTBDaily(t, db, tenantID, "2026-09-14", account); got.incrDebit != 45 || got.closeDebit != 145 {
		t.Fatalf("Monday = %+v, want incr 45 (Sat 40 + Mon 5) close 145", got)
	}
}

// Lần dựng đầu tiên của tenant: lịch sử trước ngày dựng vào số đầu kỳ, không
// vào phát sinh trong ngày, và không bị bỏ.
func TestRebuildDailyFirstRunCarriesHistoryIntoOpening(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-0000000000a4"
	const account = "TBD-HISTORY"
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()

	insertTBDailyEntry(t, db, tenantID, "2026-09-01", account, 70)
	insertTBDailyEntry(t, db, tenantID, "2026-09-03", account, 3)
	if _, err := NewTrialBalanceDailyService(db).RebuildDaily(ctx, tenantID, "2026-09-03", "test"); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	if got := readTBDaily(t, db, tenantID, "2026-09-03", account); got.incrDebit != 3 || got.closeDebit != 73 {
		t.Fatalf("first rebuilt date = %+v, want incr 3 close 73", got)
	}
}
