package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/migrations"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// operateAuditTestDB opens a throwaway Postgres given by
// WORKFLOW_TEST_DATABASE_DSN (local container only — never a shared cluster)
// and applies the embedded migrations. Without the variable the test skips.
func operateAuditTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WORKFLOW_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("WORKFLOW_TEST_DATABASE_DSN is not set")
	}
	db, err := sql.Open("pgx/v5", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ardapostgres.Migrate(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func tenantContext(tenant string) context.Context {
	return ardametadata.AppendToOutgoing(context.Background(), ardametadata.Context{TenantID: tenant, AuthChecked: "true"})
}

// TestCancelOpenCaseByProcessKeyOnlyCancelsOpenCases: hủy qua Operate chỉ đổi
// hồ sơ chưa kết thúc, ghi CASE_CANCELLED (actor, lý do) trong cùng giao dịch;
// hồ sơ COMPLETED và hồ sơ tenant khác không bị đổi.
func TestCancelOpenCaseByProcessKeyOnlyCancelsOpenCases(t *testing.T) {
	db := operateAuditTestDB(t)
	ctx := tenantContext("tenant-e4")
	repo := NewCaseRepository(db)
	if _, err := db.Exec(`
		INSERT INTO business_operation_types (case_type, business_area, operation_name, bpmn_process_id, maker_role, checker_role, owner_service)
		VALUES ('E4_TEST', 'TEST', 'E4 test', 'common-maker-checker', 'MAKER', 'CHECKER', 'test')
		ON CONFLICT (case_type) DO NOTHING`); err != nil {
		t.Fatalf("seed case type: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM business_cases WHERE case_type = 'E4_TEST'`); err != nil {
		t.Fatalf("reset cases: %v", err)
	}
	insert := func(id, tenant, status string, key int64) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO business_cases (id, tenant_id, case_type, case_code, title, primary_object_type, primary_object_id,
			                            domain_service, status, created_by, process_instance_key)
			VALUES ($1, $2, 'E4_TEST', $1, 'e4', 'x', 'x', 'test', $3, 'maker', $4)`, id, tenant, status, key); err != nil {
			t.Fatalf("seed case %s: %v", id, err)
		}
	}
	insert("e4-open", "tenant-e4", CaseStatusInReview, 9001)
	insert("e4-done", "tenant-e4", CaseStatusCompleted, 9002)
	insert("e4-other", "tenant-other", CaseStatusInReview, 9003)

	if err := repo.CancelOpenCaseByProcessKey(ctx, 9002, "op-1", "hủy"); !errors.Is(err, ErrCaseNotOpen) {
		t.Fatalf("completed cancel err = %v, want ErrCaseNotOpen", err)
	}
	if err := repo.CancelOpenCaseByProcessKey(ctx, 9003, "op-1", "hủy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other-tenant cancel err = %v, want ErrNotFound", err)
	}
	if err := repo.CancelOpenCaseByProcessKey(ctx, 9001, "op-1", "khách rút hồ sơ"); err != nil {
		t.Fatalf("open cancel err = %v", err)
	}
	status := func(id string) string {
		var s string
		if err := db.QueryRow(`SELECT status FROM business_cases WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatalf("read status %s: %v", id, err)
		}
		return s
	}
	if status("e4-open") != CaseStatusCancelled || status("e4-done") != CaseStatusCompleted || status("e4-other") != CaseStatusInReview {
		t.Fatalf("statuses = %s/%s/%s, want CANCELLED/COMPLETED/IN_REVIEW", status("e4-open"), status("e4-done"), status("e4-other"))
	}
	var from, to, actor, note string
	if err := db.QueryRow(`
		SELECT from_status, to_status, actor, note FROM case_timeline_events
		WHERE case_id = 'e4-open' AND event_type = 'CASE_CANCELLED'`).Scan(&from, &to, &actor, &note); err != nil {
		t.Fatalf("read cancel timeline: %v", err)
	}
	if from != CaseStatusInReview || to != CaseStatusCancelled || actor != "op-1" || note != "khách rút hồ sơ" {
		t.Fatalf("timeline = %s→%s %s %q", from, to, actor, note)
	}

	if err := repo.AddOperateAuditEvent(ctx, "e4-open", "OPERATE_VARIABLES_SET", "op-1", "sửa sự cố",
		map[string]any{"variables": []string{"note"}, "before": map[string]any{"note": "a"}, "after": map[string]any{"note": "b"}}); err != nil {
		t.Fatalf("operate audit: %v", err)
	}
	var after string
	if err := db.QueryRow(`
		SELECT data->'after'->>'note' FROM case_timeline_events
		WHERE case_id = 'e4-open' AND event_type = 'OPERATE_VARIABLES_SET' AND actor = 'op-1'`).Scan(&after); err != nil || after != "b" {
		t.Fatalf("operate audit row after = %q, %v", after, err)
	}
	if err := repo.AddOperateAuditEvent(ctx, "e4-other", "OPERATE_VARIABLES_SET", "op-1", "x", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other-tenant audit err = %v, want ErrNotFound", err)
	}
}
