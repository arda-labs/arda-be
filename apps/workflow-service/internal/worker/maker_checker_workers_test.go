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
	if err := ValidateMakerCheckerDependencies([]string{"lnm.recovery"}, false); err == nil {
		t.Fatal("active maker-checker worker kind must fail startup without loan-service")
	}
	if err := ValidateMakerCheckerDependencies(nil, false); err != nil {
		t.Fatalf("no shared worker kinds should not require loan-service: %v", err)
	}
	if err := ValidateMakerCheckerDependencies([]string{"lnm.recovery"}, true); err != nil {
		t.Fatalf("available loan-service should satisfy worker dependency: %v", err)
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
