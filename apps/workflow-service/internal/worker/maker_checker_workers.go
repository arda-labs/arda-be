package worker

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

const (
	JobMakerCheckerValidate = "mc.validate"
	JobMakerCheckerExecute  = "mc.execute"
	JobMakerCheckerCancel   = "mc.cancel"
)

type JobHandlerRegistration struct {
	Topic   string
	Handler worker.JobHandler
}

type makerCheckerHandlerSet struct {
	validate worker.JobHandler
	execute  worker.JobHandler
	cancel   worker.JobHandler
}

// MakerCheckerWorkers route the three static shared BPMN job types to the
// existing domain handlers selected by the service-owned mcKind variable.
type MakerCheckerWorkers struct {
	handlers map[string]makerCheckerHandlerSet
}

func NewMakerCheckerWorkers(loanWorkers *LoanWorkers, workerKinds, supportedKinds []string) (*MakerCheckerWorkers, error) {
	if loanWorkers == nil {
		return nil, fmt.Errorf("loan workers are required for maker-checker dispatch")
	}
	supported := make(map[string]struct{}, len(supportedKinds))
	for _, kind := range supportedKinds {
		supported[strings.TrimSpace(kind)] = struct{}{}
	}
	handlers := make(map[string]makerCheckerHandlerSet, len(workerKinds))
	for _, workerKind := range workerKinds {
		kind, err := normalizeMakerCheckerKind(workerKind)
		if err != nil {
			return nil, err
		}
		if _, ok := supported[kind]; !ok {
			return nil, fmt.Errorf("maker-checker worker kind %q is not supported", workerKind)
		}
		validate, execute, cancel := loanWorkers.Handlers(kind)
		handlers[kind] = makerCheckerHandlerSet{validate: validate, execute: execute, cancel: cancel}
	}
	return &MakerCheckerWorkers{handlers: handlers}, nil
}

func normalizeMakerCheckerKind(workerKind string) (string, error) {
	kind := strings.TrimSpace(workerKind)
	kind = strings.TrimPrefix(kind, "lnm.")
	if kind == "" {
		return "", fmt.Errorf("maker-checker worker kind is required")
	}
	return kind, nil
}

func resolveMakerCheckerHandlers(workerKind string, handlers map[string]makerCheckerHandlerSet) (makerCheckerHandlerSet, bool) {
	kind, err := normalizeMakerCheckerKind(workerKind)
	if err != nil {
		return makerCheckerHandlerSet{}, false
	}
	handler, ok := handlers[kind]
	return handler, ok
}

func (w *MakerCheckerWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.dispatch("validate"), w.dispatch("execute"), w.dispatch("cancel")
}

func (w *MakerCheckerWorkers) Registrations() []JobHandlerRegistration {
	validate, execute, cancel := w.Handlers()
	return []JobHandlerRegistration{
		{Topic: JobMakerCheckerValidate, Handler: validate},
		{Topic: JobMakerCheckerExecute, Handler: execute},
		{Topic: JobMakerCheckerCancel, Handler: cancel},
	}
}

func (w *MakerCheckerWorkers) dispatch(phase string) worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		vars, err := job.GetVariablesAsMap()
		if err != nil {
			throwValidationError(client, job, "invalid maker-checker process variables")
			return
		}
		workerKind, _ := vars["mcKind"].(string)
		handlers, ok := resolveMakerCheckerHandlers(workerKind, w.handlers)
		if !ok {
			slog.Error("unknown maker-checker worker kind", "workerKind", workerKind, "jobType", job.GetType())
			throwValidationError(client, job, "unsupported maker-checker worker kind")
			return
		}
		var handler worker.JobHandler
		switch phase {
		case "validate":
			handler = handlers.validate
		case "execute":
			handler = handlers.execute
		case "cancel":
			handler = handlers.cancel
		}
		if handler == nil {
			throwValidationError(client, job, "maker-checker worker phase is not registered")
			return
		}
		handler(client, job)
	}
}

// ValidateMakerCheckerDependencies fails startup rather than serving a process
// type whose static job topics have no domain worker behind them.
func ValidateMakerCheckerDependencies(workerKinds []string, loanAvailable bool) error {
	if len(workerKinds) == 0 || loanAvailable {
		return nil
	}
	return fmt.Errorf("active maker-checker worker kinds require loan-service: %s", strings.Join(workerKinds, ","))
}
