package service

import (
	"database/sql"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestJournalImmutabilityTriggers(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()

	const tenantID = "00000000-0000-0000-0000-000000000032"
	insertEntry := func(status string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`
			INSERT INTO fin_journal_entries
				(tenant_id, accounting_date, currency_code, status, business_domain, business_doc_type)
			VALUES ($1, '2026-10-08', 'VND', $2, 'fin', 'TEST')
			RETURNING id::text`, tenantID, status).Scan(&id); err != nil {
			t.Fatalf("insert %s entry: %v", status, err)
		}
		return id
	}
	insertLine := func(entryID string) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO fin_journal_lines
				(tenant_id, entry_id, line_no, direction, bal_type_code, coa_version, account_code, amount_minor, currency_code)
			VALUES ($1, $2, 1, 'DEBIT', 'ACTUAL', 'V1', '1000', 100, 'VND')`, tenantID, entryID); err != nil {
			t.Fatalf("insert journal line: %v", err)
		}
	}
	assertRejected := func(label, query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err == nil {
			t.Errorf("%s unexpectedly succeeded", label)
		}
	}

	pendingPost := insertEntry("PENDING")
	insertLine(pendingPost)
	if _, err := db.Exec(`UPDATE fin_journal_entries SET status='POSTED', posted_at=now(), updated_at=now() WHERE id=$1`, pendingPost); err != nil {
		t.Fatalf("PENDING to POSTED transition: %v", err)
	}

	pendingVoid := insertEntry("PENDING")
	if _, err := db.Exec(`UPDATE fin_journal_entries SET status='VOID', void_reason='test', idempotency_key=NULL, updated_at=now() WHERE id=$1`, pendingVoid); err != nil {
		t.Fatalf("PENDING to VOID transition: %v", err)
	}

	posted := insertEntry("POSTED")
	insertLine(posted)
	reversal := insertEntry("POSTED")
	if _, err := db.Exec(`UPDATE fin_journal_entries SET status='REVERSED', reversed_by_entry_id=$2, updated_at=now() WHERE id=$1`, posted, reversal); err != nil {
		t.Fatalf("POSTED to REVERSED transition: %v", err)
	}

	immutable := insertEntry("POSTED")
	insertLine(immutable)
	assertRejected("posted accounting date update", `UPDATE fin_journal_entries SET accounting_date='2026-10-09' WHERE id=$1`, immutable)
	assertRejected("posted entry delete", `DELETE FROM fin_journal_entries WHERE id=$1`, immutable)
	assertRejected("posted line amount update", `UPDATE fin_journal_lines SET amount_minor=200 WHERE entry_id=$1`, immutable)
	assertRejected("posted line delete", `DELETE FROM fin_journal_lines WHERE entry_id=$1`, immutable)
	invalidPending := insertEntry("PENDING")
	assertRejected("invalid PENDING data mutation", `UPDATE fin_journal_entries SET accounting_date='2026-10-09', status='POSTED', posted_at=now() WHERE id=$1`, invalidPending)
}
