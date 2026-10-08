package service

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// GATE smoke (P3a): requires the disposable PostgreSQL 18 database configured
// by ARDA_TEST_DSN. Runs finance migrations, seeds isolated opening balances, backfills
// fin_trial_balance_daily across the full journal date range, proves
// idempotent re-run, the invariant close(daily) == on-the-fly
// TrialBalance(asOf), and renders the seeded CDKT/B02 statements.
//
// See docs/postgres-integration-tests.md for the test command.
func TestReportingSmoke(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-000000000010"

	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO fin_opening_balances
			(tenant_id, accounting_date, coa_version, account_code, currency_code, direction, amount_minor, created_by)
		VALUES
			($1, '2026-09-07', 'V1', '1131', 'VND', 'DEBIT', 1000000, 'reporting-smoke'),
			($1, '2026-09-07', 'V1', '5111', 'VND', 'CREDIT', 200000, 'reporting-smoke'),
			($1, '2026-09-07', 'V1', '6321', 'VND', 'DEBIT', 50000, 'reporting-smoke')`, tenantID); err != nil {
		t.Fatalf("seed reporting smoke opening balances: %v", err)
	}

	// Date range present in the ledger (journal + opening balances).
	var d0, d1 sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT min(d)::text, max(d)::text FROM (
			SELECT e.accounting_date AS d FROM fin_journal_entries e
			  WHERE e.tenant_id = $1 AND e.status = 'POSTED'
			UNION ALL
			SELECT o.accounting_date FROM fin_opening_balances o WHERE o.tenant_id = $1
		) s`, tenantID).Scan(&d0, &d1)
	if err != nil {
		t.Fatalf("date range: %v", err)
	}
	if !d0.Valid || !d1.Valid {
		t.Fatal("reporting smoke fixture did not create journal or opening-balance data")
	}
	start, _ := time.Parse("2006-01-02", d0.String)
	end, _ := time.Parse("2006-01-02", d1.String)
	if days := int(end.Sub(start).Hours()/24) + 1; days > 400 {
		t.Fatalf("journal spans %d days — smoke caps at 400", days)
	}

	daily := NewTrialBalanceDailyService(db)

	// 1. Backfill day-by-day from the earliest ledger date (the real
	//    backfill path — openings and gaps must resolve correctly).
	var lastRows int
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		res, err := daily.RebuildDaily(ctx, tenantID, d.Format("2006-01-02"), "p3a-smoke")
		if err != nil {
			t.Fatalf("rebuild %s: %v", d.Format("2006-01-02"), err)
		}
		lastRows = res.AccountRows
	}
	if lastRows == 0 {
		t.Fatal("rebuild produced no rows for the latest date")
	}

	// 2. Idempotency: re-running the latest date reproduces identical rows.
	type row struct {
		acc, ccy           string
		openD, openC, d, c int64
		closeD, closeC     int64
	}
	snapshot := func() ([]row, error) {
		rows, err := db.QueryContext(ctx, `
			SELECT account_code, currency_code, open_debit_minor, open_credit_minor,
			       incr_debit_minor, incr_credit_minor, close_debit_minor, close_credit_minor
			FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND business_date = $2::date
			ORDER BY account_code, currency_code`, tenantID, d1.String)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.acc, &r.ccy, &r.openD, &r.openC, &r.d, &r.c, &r.closeD, &r.closeC); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	before, err := snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daily.RebuildDaily(ctx, tenantID, d1.String, "p3a-smoke"); err != nil {
		t.Fatalf("rebuild again: %v", err)
	}
	after, err := snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, after) {
		t.Fatalf("rebuild is not idempotent: %d rows before vs %d after, or values differ", len(before), len(after))
	}

	// 3. Invariant: precomputed close must equal the on-the-fly trial
	//    balance for the latest date (per account×currency net).
	tb := NewTrialBalanceService(db)
	tbr, err := tb.TrialBalance(ctx, tenantID, d1.String)
	if err != nil {
		t.Fatalf("on-the-fly trial balance: %v", err)
	}
	tbNet := map[string]int64{}
	for _, e := range tbr.Entries {
		tbNet[e.AccountCode+"|"+e.CurrencyCode] = e.BalanceMinor
	}
	closeNet := map[string]int64{}
	for _, r := range after {
		closeNet[r.acc+"|"+r.ccy] = r.closeD - r.closeC
	}
	for k, want := range tbNet {
		if closeNet[k] != want {
			t.Fatalf("invariant broken for %s: daily close %d != trial balance %d", k, closeNet[k], want)
		}
	}

	// 4. Statements render from the precompute.
	stmts := NewStatementService(db)
	cdk, err := stmts.RunStatement(ctx, tenantID, "CDKT", d1.String, "", "")
	if err != nil {
		t.Fatalf("CDKT: %v", err)
	}
	byCode := map[string]StatementRow{}
	for _, r := range cdk.Rows {
		byCode[r.RowCode] = r
	}
	var bankNet int64
	for _, r := range after {
		if r.acc == "1131" {
			bankNet += r.closeD - r.closeC
		}
	}
	if bank, ok := byCode["BANK"]; ok && bank.AmountMinor != bankNet {
		t.Fatalf("CDKT BANK = %d, want daily close of 1131 = %d", bank.AmountMinor, bankNet)
	}
	b02, err := stmts.RunStatement(ctx, tenantID, "B02", d1.String, "", "")
	if err != nil {
		t.Fatalf("B02: %v", err)
	}
	b02Rows := map[string]StatementRow{}
	for _, r := range b02.Rows {
		b02Rows[r.RowCode] = r
	}
	wantProfit := b02Rows["INCOME_TOTAL"].AmountMinor - b02Rows["EXPENSE_TOTAL"].AmountMinor
	if got := b02Rows["PROFIT"].AmountMinor; got != wantProfit {
		t.Fatalf("B02 PROFIT = %d, want income-expense = %d", got, wantProfit)
	}
	fmt.Printf("p3a smoke OK: dates %s..%s, %d accounts, CDKT ASSETS_TOTAL=%d, B02 PROFIT=%d\n",
		d0.String, d1.String, len(after), byCode["ASSETS_TOTAL"].AmountMinor, b02Rows["PROFIT"].AmountMinor)
}
