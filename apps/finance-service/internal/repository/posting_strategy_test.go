package repository

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestPostingStrategyConfigurationQueries(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	const tenantID = "00000000-0000-0000-0000-000000000034"

	for _, row := range []struct {
		from, to, start, end string
		active, deleted      bool
	}{
		{"A", "B", "2026-01-01", "", true, false},
		{"B", "A", "2026-01-01", "2026-06-30", true, false},
		{"A", "C", "2026-01-01", "", false, false},
		{"C", "A", "2026-01-01", "", true, true},
	} {
		if _, err := db.Exec(`
			INSERT INTO fin_debt_group_transitions
				(tenant_id, from_group_code, to_group_code, effective_from, effective_to, is_active, is_deleted)
			VALUES ($1, $2, $3, $4::date, NULLIF($5, '')::date, $6, $7)`, tenantID, row.from, row.to, row.start, row.end, row.active, row.deleted); err != nil {
			t.Fatalf("insert transition %s -> %s: %v", row.from, row.to, err)
		}
	}

	repo := NewPostingRepository(db)
	got, err := repo.ListDebtGroupTransitions(t.Context(), tenantID, "2026-07-01")
	if err != nil {
		t.Fatal(err)
	}
	want := []DebtGroupTransition{{FromGroupCode: "A", ToGroupCode: "B"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListDebtGroupTransitions() = %#v, want %#v", got, want)
	}

	if _, err := db.Exec(`
		INSERT INTO fin_accounting_rules (tenant_id, document_type, line_no, direction, posting_strategy)
		VALUES ($1, 'TEST', 1, 'DEBIT', 'DEBT_GROUP_RECLASS')`, tenantID); err != nil {
		t.Fatalf("insert accounting rule: %v", err)
	}
	rules, err := repo.ListRules(t.Context(), tenantID, "TEST")
	if err != nil || len(rules) != 1 || rules[0].Strategy != "DEBT_GROUP_RECLASS" {
		t.Fatalf("ListRules() = %#v, %v; want DEBT_GROUP_RECLASS", rules, err)
	}
	if _, err := db.Exec(`
		INSERT INTO fin_accounting_rules (tenant_id, document_type, line_no, direction, posting_strategy)
		VALUES ($1, 'INVALID', 1, 'DEBIT', 'UNRECOGNIZED')`, tenantID); err == nil {
		t.Fatal("unknown posting_strategy unexpectedly passed the database constraint")
	}
}
