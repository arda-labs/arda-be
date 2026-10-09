package worker

import (
	"testing"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

func TestResolveMakerCheckerHandlersUsesConfiguredDomainKind(t *testing.T) {
	noop := func(worker.JobClient, entities.Job) {}
	want := makerCheckerHandlerSet{validate: noop, execute: noop, cancel: noop}
	got, ok := resolveMakerCheckerHandlers("lnm.recovery", map[string]makerCheckerHandlerSet{"recovery": want})
	if !ok || got.validate == nil || got.execute == nil || got.cancel == nil {
		t.Fatalf("resolved handler set = %+v, %v; want configured recovery handlers", got, ok)
	}
}

func TestResolveMakerCheckerHandlersRejectsUnknownKind(t *testing.T) {
	if _, ok := resolveMakerCheckerHandlers("lnm.unknown", map[string]makerCheckerHandlerSet{}); ok {
		t.Fatal("unknown worker kind resolved")
	}
	if _, err := normalizeMakerCheckerKind(" "); err == nil {
		t.Fatal("empty worker kind was accepted")
	}
}

func TestValidateMakerCheckerDependenciesFailsClosed(t *testing.T) {
	if err := ValidateMakerCheckerDependencies([]string{"lnm.recovery"}, false, false); err == nil {
		t.Fatal("active maker-checker worker kind must fail startup without loan-service")
	}
	if err := ValidateMakerCheckerDependencies(nil, false, false); err != nil {
		t.Fatalf("no shared worker kinds should not require loan-service: %v", err)
	}
	if err := ValidateMakerCheckerDependencies([]string{"lnm.recovery"}, true, false); err != nil {
		t.Fatalf("available loan-service should satisfy worker dependency: %v", err)
	}
	if err := ValidateMakerCheckerDependencies([]string{"lnm.disb-batch-register"}, true, false); err == nil {
		t.Fatal("active batch disbursement kind must fail startup without finance-service")
	}
	if err := ValidateMakerCheckerDependencies([]string{"lnm.disb-batch-register"}, true, true); err != nil {
		t.Fatalf("available loan and finance services should satisfy batch dependency: %v", err)
	}
}

func TestMakerCheckerRegistrationsMatchRegisteredTopicManifest(t *testing.T) {
	noop := func(worker.JobClient, entities.Job) {}
	workers := &MakerCheckerWorkers{handlers: map[string]makerCheckerHandlerSet{
		"recovery": {validate: noop, execute: noop, cancel: noop},
	}}
	registrations := workers.Registrations()
	if len(registrations) != 3 {
		t.Fatalf("registrations = %d, want three", len(registrations))
	}
	for i, want := range []string{JobMakerCheckerValidate, JobMakerCheckerExecute, JobMakerCheckerCancel} {
		if registrations[i].Topic != want || registrations[i].Handler == nil {
			t.Fatalf("registration %d = %+v, want topic %s with handler", i, registrations[i], want)
		}
	}
	for _, topic := range []string{JobMakerCheckerValidate, JobMakerCheckerExecute, JobMakerCheckerCancel} {
		found := false
		for _, registered := range RegisteredJobTopics {
			if topic == registered {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("maker-checker topic %q missing from RegisteredJobTopics", topic)
		}
	}
}

func TestMakerCheckerRegistersSupportedKindsBeforeAnyOperationIsConfigured(t *testing.T) {
	loanWorkers := NewLoanWorkers(nil, nil)
	workers, err := NewMakerCheckerWorkers(loanWorkers, nil, []string{"recovery"})
	if err != nil {
		t.Fatalf("NewMakerCheckerWorkers() error = %v", err)
	}
	if _, ok := resolveMakerCheckerHandlers("lnm.recovery", workers.handlers); !ok {
		t.Fatal("supported recovery kind must be dispatchable before a maker-checker operation is configured")
	}
	for _, registration := range workers.Registrations() {
		if registration.Handler == nil {
			t.Fatalf("topic %q has no handler", registration.Topic)
		}
	}
}

func TestMakerCheckerWorkersAcceptsBatchFlowHandlers(t *testing.T) {
	noop := func(worker.JobClient, entities.Job) {}
	workers, err := NewMakerCheckerWorkersWithAdditional(nil,
		[]string{"lnm.disb-batch-register"}, nil,
		map[string]MakerCheckerHandlers{
			"lnm.disb-batch-register": {Validate: noop, Execute: noop, Cancel: noop},
		},
	)
	if err != nil {
		t.Fatalf("construct batch maker-checker workers: %v", err)
	}
	got, ok := resolveMakerCheckerHandlers("lnm.disb-batch-register", workers.handlers)
	if !ok || got.validate == nil || got.execute == nil || got.cancel == nil {
		t.Fatalf("batch handlers = %+v, %v; want validate/execute/cancel", got, ok)
	}
}
