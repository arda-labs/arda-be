package migration

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// TestHRMEmployeeRegisterCanaryAppliesAndRollsBack runs the manual ADR-011
// pilot script against a freshly migrated workflow database: Up routes the
// operation type to the shared process, Down restores it.
func TestHRMEmployeeRegisterCanaryAppliesAndRollsBack(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return Run(db, "postgres") })
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "canary", "20261012100000_workflow_hrm_employee_register_common_maker_checker_canary.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(raw), "-- +goose Down")
	if !ok {
		t.Fatal("canary script has no Down section")
	}

	route := func() (process, kind string) {
		t.Helper()
		var workerKind sql.NullString
		if err := db.QueryRow(`SELECT bpmn_process_id, worker_kind FROM business_operation_types WHERE case_type = 'HRM_EMPLOYEE_REGISTRATION' AND status = 'ACTIVE'`).Scan(&process, &workerKind); err != nil {
			t.Fatal(err)
		}
		return process, workerKind.String
	}
	if process, kind := route(); process != "hrm-employee-registration-v2" || kind != "" {
		t.Fatalf("seeded route = %s/%q", process, kind)
	}
	if _, err := db.Exec(up); err != nil {
		t.Fatalf("canary Up: %v", err)
	}
	if process, kind := route(); process != "common-maker-checker" || kind != "hrm.employee-register" {
		t.Fatalf("route after Up = %s/%q", process, kind)
	}
	if _, err := db.Exec(up); err != nil {
		t.Fatalf("canary Up must be re-runnable: %v", err)
	}
	if _, err := db.Exec(down); err != nil {
		t.Fatalf("canary Down: %v", err)
	}
	if process, kind := route(); process != "hrm-employee-registration-v2" || kind != "" {
		t.Fatalf("route after Down = %s/%q", process, kind)
	}
}
