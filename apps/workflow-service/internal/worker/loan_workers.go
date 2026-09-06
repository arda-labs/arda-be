package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// LoanWorkers dispatch the uniform loan adjustment jobs
// (`lnm.<kind>.validate|execute|cancel`) to loan-service over gRPC. One
// handler triple per registered kind — the flow logic lives in loan-service,
// this is pure transport.
type LoanWorkers struct {
	loanClient *loanclient.Client
	projection *CaseProjection
}

func NewLoanWorkers(loanClient *loanclient.Client, caseRepo *repository.CaseRepository) *LoanWorkers {
	return &LoanWorkers{loanClient: loanClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers for one kind.
func (w *LoanWorkers) Handlers(kind string) (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validateHandler(kind), w.executeHandler(kind), w.cancelHandler(kind)
}

func (w *LoanWorkers) adjustmentID(job entities.Job) (string, bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("loan worker: invalid variables", "err", err)
		return "", false
	}
	adjustmentID, _ := vars["adjustmentId"].(string)
	if adjustmentID == "" {
		adjustmentID, _ = vars["primaryObjectId"].(string)
	}
	if adjustmentID == "" {
		slog.Warn("loan worker: missing adjustmentId", "jobType", job.GetType())
		return "", false
	}
	return adjustmentID, true
}

func (w *LoanWorkers) validateHandler(kind string) worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob(kind, "validate", job)
		id, ok := w.adjustmentID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		ok, message, err := w.loanClient.CheckAdjustment(crmJobContext(job), kind, id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if !ok {
			throwValidationError(client, job, message)
			return
		}
		_ = w.completeJob(client, job, nil)
	}
}

func (w *LoanWorkers) executeHandler(kind string) worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob(kind, "execute", job)
		id, ok := w.adjustmentID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if err := w.loanClient.ResolveAdjustment(crmJobContext(job), kind, id, "APPROVE", decidedBy, note); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{
			"approvalStatus": "APPROVED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *LoanWorkers) cancelHandler(kind string) worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob(kind, "cancel", job)
		id, ok := w.adjustmentID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if note == "" {
			note = "Rejected by checker"
		}
		if err := w.loanClient.ResolveAdjustment(crmJobContext(job), kind, id, "REJECT", decidedBy, note); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{
			"approvalStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *LoanWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) > 0 {
		withVars, err := cmd.VariablesFromMap(result)
		if err != nil {
			return err
		}
		_, err = withVars.Send(context.Background())
		return err
	}
	_, err := cmd.Send(context.Background())
	return err
}

func (w *LoanWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow loan job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("failed to fail loan job", "jobKey", job.GetKey(), "err", err)
	}
}

func logLoanJob(kind, phase string, job entities.Job) {
	vars, _ := job.GetVariablesAsMap()
	adjustmentID, _ := vars["adjustmentId"].(string)
	slog.Info("loan job", "kind", kind, "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey(), "adjustmentId", adjustmentID)
}

// throwValidationError is shared with the CRM register workers.
func throwValidationError(client worker.JobClient, job entities.Job, message string) {
	slog.Warn("workflow validation failed", "jobKey", job.GetKey(), "reason", message)
	_, err := client.NewThrowErrorCommand().
		JobKey(job.GetKey()).
		ErrorCode(ErrorValidationFailed).
		ErrorMessage(message).
		Send(context.Background())
	if err != nil {
		slog.Error("failed to throw validation error", "jobKey", job.GetKey(), "err", err)
	}
}
