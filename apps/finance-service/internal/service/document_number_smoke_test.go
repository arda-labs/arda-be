package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-docno"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestDocumentNumberIssueConcurrencyRollbackAndRenumber(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-000000000010"
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO fin_periods (tenant_id, period_code, start_date, end_date, status) VALUES ('` + tenantID + `','2026-10','2026-10-01','2026-10-31','OPEN')`,
		`INSERT INTO fin_periods (tenant_id, period_code, start_date, end_date, status) VALUES ('` + tenantID + `','2026-09','2026-09-01','2026-09-30','CLOSED')`,
		`INSERT INTO doc_series (id,tenant_id,document_type,version,effective_from,reset_period,pattern,sequence_start,sequence_width,status) VALUES ('00000000-0000-0000-0000-000000000101','` + tenantID + `','TEST',1,'2026-01-01','MONTH','T-{YYYY}{MM}-{SEQ:4}',1,4,'ACTIVE')`,
		`INSERT INTO doc_series (id,tenant_id,document_type,version,effective_from,reset_period,pattern,sequence_start,sequence_width,status) VALUES ('00000000-0000-0000-0000-000000000102','` + tenantID + `','ROLLBACK',1,'2026-01-01','MONTH','R-{YYYY}{MM}-{SEQ:4}',1,4,'ACTIVE')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	series := docno.Series{ID: "00000000-0000-0000-0000-000000000101", TenantID: tenantID, DocumentType: "TEST", Version: 1, EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ResetPeriod: docno.ResetMonth, Pattern: "T-{YYYY}{MM}-{SEQ:4}", SequenceStart: 1, SequenceWidth: 4, OverflowPolicy: "REJECT", Status: "ACTIVE"}
	date := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	const count = 12
	var wg sync.WaitGroup
	errCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errCh <- err
				return
			}
			_, err = docno.Issue(ctx, tx, series, fmt.Sprintf("parallel-%d", i), date)
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	var issued int
	if err := db.QueryRowContext(ctx, `SELECT count(DISTINCT display_no) FROM document_number WHERE tenant_id=$1 AND series_id=$2`, tenantID, series.ID).Scan(&issued); err != nil || issued != count {
		t.Fatalf("distinct numbers=%d err=%v want %d", issued, err, count)
	}

	rollbackSeries := series
	rollbackSeries.ID = "00000000-0000-0000-0000-000000000102"
	rollbackSeries.DocumentType = "ROLLBACK"
	rollbackSeries.Pattern = "R-{YYYY}{MM}-{SEQ:4}"
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docno.Issue(ctx, tx, rollbackSeries, "rolled-back", date); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := docno.Issue(ctx, tx, rollbackSeries, "after-rollback", date)
	if err != nil {
		t.Fatal(err)
	}
	if first.SequenceNo != 1 {
		t.Fatalf("sequence after rollback=%d want 1", first.SequenceNo)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// A journal row is an immutable sentinel: document-number approval must
	// not alter its accounting date, amounts, accounts, or posting status.
	var entryID string
	if err = db.QueryRowContext(ctx, `INSERT INTO fin_journal_entries (tenant_id,accounting_date,currency_code,business_domain,business_doc_type,description) VALUES ($1,'2026-10-09','VND','test','TEST','immutable') RETURNING id::text`, tenantID).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	for _, line := range []struct {
		no            int
		side, account string
	}{{1, "DEBIT", "1001"}, {2, "CREDIT", "2001"}} {
		if _, err = db.ExecContext(ctx, `INSERT INTO fin_journal_lines (tenant_id,entry_id,line_no,direction,coa_version,account_code,amount_minor,currency_code) VALUES ($1,$2,$3,$4,'V1',$5,1000,'VND')`, tenantID, entryID, line.no, line.side, line.account); err != nil {
			t.Fatal(err)
		}
	}
	ledgerHash := func() string {
		rows, e := db.QueryContext(ctx, `SELECT e.accounting_date::text||'|'||l.account_code||'|'||l.amount_minor::text||'|'||l.direction FROM fin_journal_entries e JOIN fin_journal_lines l ON l.entry_id=e.id WHERE e.tenant_id=$1 ORDER BY e.entry_no,l.line_no`, tenantID)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		var buf string
		for rows.Next() {
			var v string
			if e := rows.Scan(&v); e != nil {
				t.Fatal(e)
			}
			buf += v
		}
		sum := sha256.Sum256([]byte(buf))
		return hex.EncodeToString(sum[:])
	}
	beforeHash := ledgerHash()
	svc := NewDocumentNumberService(db)
	request, err := svc.RequestRenumber(ctx, tenantID, "parallel-0", "maker-1", "T-202610-9999", "correction approved by finance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApproveRenumber(ctx, tenantID, request.ID, "maker-1", false); err != ErrRenumberMakerChecker {
		t.Fatalf("maker self-approval error=%v", err)
	}
	if _, err = svc.ApproveRenumber(ctx, tenantID, request.ID, "checker-2", false); err != nil {
		t.Fatal(err)
	}
	if after := ledgerHash(); after != beforeHash {
		t.Fatalf("ledger hash changed on renumber: before=%s after=%s", beforeHash, after)
	}
	var aliasStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM doc_number_alias WHERE tenant_id=$1 AND display_no='T-202610-0001'`, tenantID).Scan(&aliasStatus); err != nil || aliasStatus != "RETIRED" {
		t.Fatalf("old-number alias status=%q err=%v", aliasStatus, err)
	}
	resolved, err := docno.Lookup(ctx, db, tenantID, "T-202610-0001")
	if err != nil || resolved.DocumentID != "parallel-0" || resolved.DisplayNo != "T-202610-9999" {
		t.Fatalf("old alias lookup=%+v err=%v", resolved, err)
	}
	if _, err = svc.RequestRenumber(ctx, tenantID, "after-rollback", "maker-1", "R-202609-0002", "closed-period correction"); err != nil {
		t.Fatal(err)
	}
	var closedDocDate string
	if err = db.QueryRowContext(ctx, `UPDATE document_number SET business_date='2026-09-15' WHERE tenant_id=$1 AND document_id='after-rollback' RETURNING business_date::text`, tenantID).Scan(&closedDocDate); err != nil {
		t.Fatal(err)
	}
	var closedReq string
	if err = db.QueryRowContext(ctx, `SELECT id::text FROM doc_number_change WHERE tenant_id=$1 AND document_id='after-rollback'`, tenantID).Scan(&closedReq); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApproveRenumber(ctx, tenantID, closedReq, "checker-2", false); err != ErrRenumberClosedPeriod {
		t.Fatalf("closed-period approval error=%v", err)
	}
	if _, err = svc.ApproveRenumber(ctx, tenantID, closedReq, "checker-2", true); err != nil {
		t.Fatal(err)
	}
}
