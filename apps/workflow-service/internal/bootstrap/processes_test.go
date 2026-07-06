package bootstrap_test

import (
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/bootstrap"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

func TestBuiltInCustomerRegistrationProcessID(t *testing.T) {
	processes := bootstrap.BuiltInProcesses()
	want := map[string]string{
		"CUSTOMER_REGISTRATION":     "crm-customer-registration-v2",
		"CUSTOMER_REGISTRATION_V1":  "customer-registration-v1",
		"CUSTOMER_ADJUSTMENT":       "customer-adjustment-v1",
		"HRM_EMPLOYEE_REGISTRATION": "hrm-employee-registration-v1",
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
