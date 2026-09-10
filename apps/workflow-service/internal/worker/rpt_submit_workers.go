package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// RPTSubmitWorkers handle the rpt-submit-v2 flow. The submission lifecycle
// lives in statistical-service: validate re-checks the staged submission and
// execute/cancel write the checker decision back via gRPC.
type RPTSubmitWorkers struct {
	statistical StatisticalSubmissionResolver
	projection  *CaseProjection
}

// StatisticalSubmissionResolver is the narrow callback surface
// (libs/go/arda-grpc/client/statistical).
type StatisticalSubmissionResolver interface {
	CheckSubmission(ctx context.Context, submissionID string) (bool, string, error)
	ResolveSubmission(ctx context.Context, submissionID, decision, actor, note string) error
}

func NewRPTSubmitWorkers(statistical StatisticalSubmissionResolver, caseRepo *repository.CaseRepository) *RPTSubmitWorkers {
	return &RPTSubmitWorkers{statistical: statistical, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *RPTSubmitWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *RPTSubmitWorkers) submissionID(job entities.Job) (string, bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("rpt submit worker: invalid variables", "err", err)
		return "", false
	}
	id, _ := vars["submissionId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		slog.Warn("rpt submit worker: missing submissionId", "jobType", job.GetType())
		return "", false
	}
	return id, true
}

func (w *RPTSubmitWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.submissionID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		valid, message, err := w.statistical.CheckSubmission(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "Statistical Error: "+err.Error())
			return
		}
		if !valid {
			throwValidationError(client, job, message)
			return
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("rpt submit validate complete failed", "jobKey", job.GetKey(), "err", err)
		}
	}
}

func (w *RPTSubmitWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.submissionID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if err := w.statistical.ResolveSubmission(crmJobContext(job), id, "APPROVE", actor, note); err != nil {
			w.failJob(client, job, "Statistical Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *RPTSubmitWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.submissionID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if note == "" {
			note = "Rejected by checker"
		}
		if err := w.statistical.ResolveSubmission(crmJobContext(job), id, "REJECT", actor, note); err != nil {
			w.failJob(client, job, "Statistical Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *RPTSubmitWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *RPTSubmitWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow rpt submit job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("rpt submit fail-job send", "err", err)
	}
}
