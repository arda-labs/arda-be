package bootstrap_test

import (
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/bootstrap"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

func TestBuiltInCustomerRegistrationProcessID(t *testing.T) {
	processes := bootstrap.BuiltInProcesses()
	want := map[string]string{
		"LNM_DISB_REGISTER_V2":      "lnm-disbursement-register-v2",
		"LNM_DISB_COMPLETE_V2":      "lnm-disbursement-complete-v2",
		"LNM_COLLECTION_V2":         "lnm-collection-v2",
		"HRM_EMPLOYEE_REGISTRATION": "hrm-employee-registration-v2",
		"DPM_SETTLE_V2":             "dpm-settle-v2",
		"RPT_SUBMIT_V2":             "rpt-submit-v2",
		"CUSTOMER_REGISTRATION": "crm-customer-registration-v2",
		"CUSTOMER_ADJUSTMENT":   "customer-adjustment-v2",
		"LOAN_FORMATION_V2":     "lnm-loan-formation-v2",
		"LNM_DEBT_CHANGE_V2":        "lnm-debt-change-v2",
		"LNM_RATE_CHANGE_V2":        "lnm-rate-change-v2",
		"LNM_RESTRUCTURE_V2":        "lnm-restructure-v2",
		"LNM_WAIVER_V2":             "lnm-waiver-v2",
		"LNM_WRITEOFF_V2":           "lnm-writeoff-v2",
		"LNM_RECOVERY_V2":           "lnm-recovery-v2",
		"LNM_FUND_CHECK_V2":         "lnm-fund-check-v2",
		"LNM_REVENUE_ALLOCATION_V2": "lnm-revenue-allocation-v2",
		"LNM_VFU_FEE_ALLOCATION_V2": "lnm-vfu-fee-allocation-v2",
		"LNM_OFF_BALANCE_EXPORT_V2": "lnm-off-balance-export-v2",
	}
	if len(processes) != len(want) {
		t.Fatalf("BuiltInProcesses() len = %d, want %d", len(processes), len(want))
	}
	for _, process := range processes {
		got, err := repository.ExtractBPMNProcessID(process.Content)
		if err != nil {
			t.Fatalf("ExtractBPMNProcessID(%s) error = %v", process.ProcessCode, err)
		}
		if got != want[process.ProcessCode] {
			t.Fatalf("process id for %s = %q, want %q", process.ProcessCode, got, want[process.ProcessCode])
		}
	}
}

func TestLoanFormationMultiLevelApproval(t *testing.T) {
	content := builtInProcessContent(t, "LOAN_FORMATION_V2")
	for _, fragment := range []string{
		`candidateGroups="LNM_MAKER"`,
		`candidateGroups="LNM_TWTD"`,
		`candidateGroups="LNM_POGD"`,
		`candidateGroups="LNM_GIDO"`,
		`candidateGroups="LNM_HODO"`,
		`sourceRef="GW_ApprovalLevel" targetRef="UT_BoardReview"`,
		`sourceRef="GW_ApprovalLevel" targetRef="UT_GDReview"`,
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("loan formation process missing %q", fragment)
		}
	}
}

func TestCustomerRegistrationStartsWithMakerRevise(t *testing.T) {
	content := builtInProcessContent(t, "CUSTOMER_REGISTRATION")
	if !strings.Contains(content, `sourceRef="Start_Submitted" targetRef="UT_MakerRevise"`) {
		t.Fatal("customer registration must start at maker revise task")
	}
	if strings.Contains(content, `sourceRef="Start_Submitted" targetRef="ST_Validate"`) {
		t.Fatal("customer registration must not go directly from start to validation")
	}
}

func TestCustomerAdjustmentStartsWithMakerRevise(t *testing.T) {
	content := builtInProcessContent(t, "CUSTOMER_ADJUSTMENT")
	if !strings.Contains(content, `sourceRef="Start_Submitted" targetRef="UT_MakerRevise"`) {
		t.Fatal("customer adjustment must start at maker revise task")
	}
	if strings.Contains(content, `sourceRef="Start_Submitted" targetRef="UT_CheckerReview"`) {
		t.Fatal("customer adjustment must not go directly from start to checker review")
	}
}

func builtInProcessContent(t *testing.T, processCode string) string {
	t.Helper()
	for _, process := range bootstrap.BuiltInProcesses() {
		if process.ProcessCode == processCode {
			return string(process.Content)
		}
	}
	t.Fatalf("%s process not found", processCode)
	return ""
}
