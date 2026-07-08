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
		"CUSTOMER_REGISTRATION": "crm-customer-registration-v2",
		"CUSTOMER_ADJUSTMENT":   "customer-adjustment-v2",
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
