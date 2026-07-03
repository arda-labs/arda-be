package bootstrap_test

import (
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/bootstrap"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

func TestBuiltInCustomerRegistrationProcessID(t *testing.T) {
	processes := bootstrap.BuiltInProcesses()
	if len(processes) != 1 {
		t.Fatalf("BuiltInProcesses() len = %d, want 1", len(processes))
	}
	got, err := repository.ExtractBPMNProcessID(processes[0].Content)
	if err != nil {
		t.Fatalf("ExtractBPMNProcessID() error = %v", err)
	}
	if got != "customer-registration-v1" {
		t.Fatalf("process id = %q, want customer-registration-v1", got)
	}
}
