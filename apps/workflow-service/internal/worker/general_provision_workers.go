package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// GeneralProvisionWorkers dispatch the lnm-general-provision-v2 jobs
// (LNM.307.01): validate checks the period, execute resolves APPROVE (the
// loan service recomputes + posts + marks POSTED), cancel resolves REJECT.
type GeneralProvisionWorkers struct {
	loanClient *loanclient.Client
	projection *CaseProjection
}

func NewGeneralProvisionWorkers(loanClient *loanclient.Client, caseRepo *repository.CaseRepository) *GeneralProvisionWorkers {
	return &GeneralProvisionWorkers{loanClient: loanClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *GeneralProvisionWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *GeneralProvisionWorkers) provisionID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["generalProvisionId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing generalProvisionId variable")
	}
	return id, nil
}

func (w *GeneralProvisionWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logGeneralProvisionJob("validate", job)
		id, err := w.provisionID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		ok, message, err := w.loanClient.CheckGeneralProvision(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if !ok {
			throwValidationError(client, job, message)
			return
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("general provision validate complete failed", "id", id, "err", err)
		}
	}
}

func (w *GeneralProvisionWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logGeneralProvisionJob("execute", job)
		id, err := w.provisionID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if err := w.loanClient.ResolveGeneralProvision(crmJobContext(job), id, "APPROVE", decidedBy, note); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *GeneralProvisionWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logGeneralProvisionJob("cancel", job)
		id, err := w.provisionID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if note == "" {
			note = "Rejected by checker"
		}
		if err := w.loanClient.ResolveGeneralProvision(crmJobContext(job), id, "REJECT", decidedBy, note); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *GeneralProvisionWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *GeneralProvisionWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow general provision job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("general provision fail-job send", "err", err)
	}
}

func logGeneralProvisionJob(phase string, job entities.Job) {
	slog.Info("general provision job", "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey())
}
