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

// SpecificProvisionWorkers dispatch the lnm-general-provision-v2 jobs
// (LNM.306.01): validate checks the period, execute resolves APPROVE (the
// loan service recomputes + posts + marks POSTED), cancel resolves REJECT.
type SpecificProvisionWorkers struct {
	loanClient *loanclient.Client
	projection *CaseProjection
}

func NewSpecificProvisionWorkers(loanClient *loanclient.Client, caseRepo *repository.CaseRepository) *SpecificProvisionWorkers {
	return &SpecificProvisionWorkers{loanClient: loanClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *SpecificProvisionWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *SpecificProvisionWorkers) provisionID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["specificProvisionId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing specificProvisionId variable")
	}
	return id, nil
}

func (w *SpecificProvisionWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logSpecificProvisionJob("validate", job)
		id, err := w.provisionID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		ok, message, err := w.loanClient.CheckSpecificProvision(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if !ok {
			throwValidationError(client, job, message)
			return
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("specific provision validate complete failed", "id", id, "err", err)
		}
	}
}

func (w *SpecificProvisionWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logSpecificProvisionJob("execute", job)
		id, err := w.provisionID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if err := w.loanClient.ResolveSpecificProvision(crmJobContext(job), id, "APPROVE", decidedBy, note); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *SpecificProvisionWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logSpecificProvisionJob("cancel", job)
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
		if err := w.loanClient.ResolveSpecificProvision(crmJobContext(job), id, "REJECT", decidedBy, note); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *SpecificProvisionWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *SpecificProvisionWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow specific provision job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("specific provision fail-job send", "err", err)
	}
}

func logSpecificProvisionJob(phase string, job entities.Job) {
	slog.Info("specific provision job", "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey())
}
