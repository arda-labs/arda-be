package migration

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

// canaryRoute describes one manual ADR-011 route script: the case types it
// moves, the legacy process they come from and the worker kind they get.
type canaryRoute struct {
	script    string
	caseTypes []string
	legacy    string
	kind      string
}

// runCanary applies the script's Up against a freshly migrated workflow
// database, re-applies it, then runs Down and checks the seeded route is
// restored for every case type the script covers.
func runCanary(t *testing.T, route canaryRoute) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return Run(db, "postgres") })
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "canary", route.script))
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(raw), "-- +goose Down")
	if !ok {
		t.Fatal("canary script has no Down section")
	}

	routeOf := func(caseType string) (process, kind string) {
		t.Helper()
		var workerKind sql.NullString
		if err := db.QueryRow(`SELECT bpmn_process_id, worker_kind FROM business_operation_types WHERE case_type = $1 AND status = 'ACTIVE'`, caseType).Scan(&process, &workerKind); err != nil {
			t.Fatalf("%s: %v", caseType, err)
		}
		return process, workerKind.String
	}
	expect := func(stage, wantProcess, wantKind string) {
		t.Helper()
		for _, caseType := range route.caseTypes {
			if process, kind := routeOf(caseType); process != wantProcess || kind != wantKind {
				t.Fatalf("%s route %s = %s/%q, want %s/%q", caseType, stage, process, kind, wantProcess, wantKind)
			}
		}
	}
	expect("seeded", route.legacy, "")
	if _, err := db.Exec(up); err != nil {
		t.Fatalf("canary Up: %v", err)
	}
	expect("after Up", "common-maker-checker", route.kind)
	if _, err := db.Exec(up); err != nil {
		t.Fatalf("canary Up must be re-runnable: %v", err)
	}
	if _, err := db.Exec(down); err != nil {
		t.Fatalf("canary Down: %v", err)
	}
	expect("after Down", route.legacy, "")
}

// TestHRMEmployeeRegisterCanaryAppliesAndRollsBack runs the manual ADR-011
// pilot script against a freshly migrated workflow database: Up routes the
// operation type to the shared process, Down restores it.
func TestHRMEmployeeRegisterCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261012100000_workflow_hrm_employee_register_common_maker_checker_canary.sql",
		caseTypes: []string{"HRM_EMPLOYEE_REGISTRATION"},
		legacy:    "hrm-employee-registration-v2",
		kind:      "hrm.employee-register",
	})
}

func TestCRMCustomerRegisterCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090000_workflow_crm_customer_register_common_maker_checker_canary.sql",
		caseTypes: []string{"CUSTOMER_REGISTRATION"},
		legacy:    "crm-customer-registration-v2",
		kind:      "crm.customer-register",
	})
}
