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
		"LNM_DISB_REGISTER_V2":        "lnm-disbursement-register-v2",
		"LNM_DISB_COMPLETE_V2":        "lnm-disbursement-complete-v2",
		"LNM_COLLECTION_V2":           "lnm-collection-v2",
		"LNM_DISB_BATCH_REGISTER_V2":  "lnm-disb-batch-register-v2",
		"LNM_DISB_BATCH_COMPLETE_V2":  "lnm-disb-batch-complete-v2",
		"LNM_COLLECTION_BATCH_V2":     "lnm-collection-batch-v2",
		"HRM_EMPLOYEE_REGISTRATION": "hrm-employee-registration-v2",
		"DPM_SETTLE_V2":             "dpm-settle-v2",
		"RPT_SUBMIT_V2":             "rpt-submit-v2",
		"FIN_SINGLE_ENTRY_V2":       "fin-single-entry-v2",
		"FIN_DOUBLE_ENTRY_V2":       "fin-double-entry-v2",
		"FIN_OFF_BALANCE_V2":        "fin-off-balance-v2",
		"FIN_TXN_CANCEL_V2":         "fin-txn-cancel-v2",
		"FIN_CLOSING_V2":            "fin-closing-v2",
		"FIN_FUND_APPROP_V2":        "fin-fund-appropriation-v2",
		"FIN_FUND_USE_V2":           "fin-fund-utilization-v2",
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
		"LNM_GENERAL_PROVISION_V2":  "lnm-general-provision-v2",
		"LNM_MORTGAGE_ADJUST_V2":    "lnm-mortgage-adjust-v2",
		"LNM_SPECIFIC_PROVISION_V1": "lnm-specific-provision-v1",
		"DPM_ADDITIONAL_V1":         "dpm-additional-v1",
		"DPM_PRODUCT_REGISTER_V1":   "dpm-product-register-v1",
		"DPM_PRODUCT_EDIT_V1":       "dpm-product-edit-v1",
		"CFC_CONTRACT_V1":           "cfc-contract-v1",
		"CFC_AMENDMENT_V1":          "cfc-amendment-v1",
		"CFC_MOVEMENT_V1":           "cfc-movement-v1",
		"IBM_PLACE_V1":              "ibm-place-v1",
		"IBM_MOVEMENT_V1":           "ibm-movement-v1",
		"DPM_RATE_V1":               "dpm-rate-v1",
		"DPM_INTEREST_V1":           "dpm-interest-v1",
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

func TestManualPostingFlowSkeleton(t *testing.T) {
	for _, processCode := range []string{"FIN_SINGLE_ENTRY_V2", "FIN_DOUBLE_ENTRY_V2", "FIN_OFF_BALANCE_V2", "FIN_TXN_CANCEL_V2", "FIN_CLOSING_V2", "FIN_FUND_APPROP_V2", "FIN_FUND_USE_V2"} {
		content := builtInProcessContent(t, processCode)
		for _, fragment := range []string{
			`candidateGroups="FIN_MAKER"`,
			`candidateGroups="FIN_CHECKER"`,
			`<zeebe:header key="stepCode" value="maker_input" />`,
			`<zeebe:header key="stepCode" value="checker_review" />`,
			`sourceRef="GW_Decision" targetRef="ST_Execute"`,
			`sourceRef="GW_Decision" targetRef="ST_Cancel"`,
			`sourceRef="GW_Decision" targetRef="UT_MakerInput"`,
			`sourceRef="Start_Submitted" targetRef="ST_Init"`,
		} {
			if !strings.Contains(content, fragment) {
				t.Fatalf("%s process missing %q", processCode, fragment)
			}
		}
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
