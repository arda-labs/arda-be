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

func TestCRMMemberRequestCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090100_workflow_crm_member_request_common_maker_checker_canary.sql",
		caseTypes: []string{"CRM_MEMBER_V1"},
		legacy:    "crm-member-v1",
		kind:      "crm.member-request",
	})
}

func TestRPTSubmitCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090200_workflow_rpt_submit_common_maker_checker_canary.sql",
		caseTypes: []string{"RPT_SUBMIT_V2"},
		legacy:    "rpt-submit-v2",
		kind:      "rpt.submit",
	})
}

func TestCFCContractCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090300_workflow_cfc_contract_common_maker_checker_canary.sql",
		caseTypes: []string{"CFC_CONTRACT_V1"},
		legacy:    "cfc-contract-v1",
		kind:      "cfc.contract",
	})
}

func TestCFCAmendmentCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090400_workflow_cfc_amendment_common_maker_checker_canary.sql",
		caseTypes: []string{"CFC_AMENDMENT_V1"},
		legacy:    "cfc-amendment-v1",
		kind:      "cfc.amendment",
	})
}

func TestCFCMovementCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090500_workflow_cfc_movement_common_maker_checker_canary.sql",
		caseTypes: []string{"CFC_MOVEMENT_V1"},
		legacy:    "cfc-movement-v1",
		kind:      "cfc.movement",
	})
}

func TestDPMSettleCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090600_workflow_dpm_settle_common_maker_checker_canary.sql",
		caseTypes: []string{"DPM_SETTLE_V2"},
		legacy:    "dpm-settle-v2",
		kind:      "dpm.settle",
	})
}

func TestDPMProductRequestCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090700_workflow_dpm_product_request_common_maker_checker_canary.sql",
		caseTypes: []string{"DPM_PRODUCT_REGISTER_V1"},
		legacy:    "dpm-product-register-v1",
		kind:      "dpm.product-request",
	})
}

func TestDPMRateRequestCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090800_workflow_dpm_rate_request_common_maker_checker_canary.sql",
		caseTypes: []string{"DPM_RATE_REGISTER_V1", "DPM_RATE_EDIT_V1"},
		legacy:    "dpm-rate-v1",
		kind:      "dpm.rate-request",
	})
}

func TestDPMInterestOpCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013090900_workflow_dpm_interest_op_common_maker_checker_canary.sql",
		caseTypes: []string{"DPM_PAY_INTEREST_V1", "DPM_CAPITALIZE_V1"},
		legacy:    "dpm-interest-v1",
		kind:      "dpm.interest-op",
	})
}

func TestDPMInterestBatchCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013091000_workflow_dpm_interest_batch_common_maker_checker_canary.sql",
		caseTypes: []string{"DPM_BATCH_INTEREST_V1"},
		legacy:    "dpm-interest-v1",
		kind:      "dpm.interest-batch",
	})
}

func TestIBMPlaceCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013091100_workflow_ibm_place_common_maker_checker_canary.sql",
		caseTypes: []string{"IBM_PLACE_V1"},
		legacy:    "ibm-place-v1",
		kind:      "ibm.place",
	})
}

func TestIBMBorrowCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013091200_workflow_ibm_borrow_common_maker_checker_canary.sql",
		caseTypes: []string{"IBM_BORROW_V1"},
		legacy:    "ibm-place-v1",
		kind:      "ibm.borrow",
	})
}

func TestIBMTopUpCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013091300_workflow_ibm_top_up_common_maker_checker_canary.sql",
		caseTypes: []string{"IBM_TOP_UP_V1"},
		legacy:    "ibm-movement-v1",
		kind:      "ibm.top-up",
	})
}

func TestIBMInterestCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013091400_workflow_ibm_interest_common_maker_checker_canary.sql",
		caseTypes: []string{"IBM_INTEREST_V1"},
		legacy:    "ibm-movement-v1",
		kind:      "ibm.interest",
	})
}

func TestIBMExpectedCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013091500_workflow_ibm_expected_common_maker_checker_canary.sql",
		caseTypes: []string{"IBM_EXPECTED_V1"},
		legacy:    "ibm-movement-v1",
		kind:      "ibm.expected",
	})
}

func TestIBMWithdrawCanaryAppliesAndRollsBack(t *testing.T) {
	runCanary(t, canaryRoute{
		script:    "20261013091600_workflow_ibm_withdraw_common_maker_checker_canary.sql",
		caseTypes: []string{"IBM_WITHDRAW_V1"},
		legacy:    "ibm-movement-v1",
		kind:      "ibm.withdraw",
	})
}
