package worker

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// RPTSubmitWorkers handle the rpt-submit-v2 flow. Validation and execution
// are terminal: the submission lifecycle lives in statistical-service and
// case completion is projected here — no domain gRPC callback needed yet.
type RPTSubmitWorkers struct {
	projection *CaseProjection
}

func NewRPTSubmitWorkers(caseRepo *repository.CaseRepository) *RPTSubmitWorkers {
	return &RPTSubmitWorkers{projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *RPTSubmitWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *RPTSubmitWorkers) submissionID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["submissionId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing submissionId variable")
	}
	return id, nil
}

func (w *RPTSubmitWorkers) pass() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		if _, err := job.GetVariablesAsMap(); err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		_ = w.complete(context.Background(), client, job, nil)
	}
}

func (w *RPTSubmitWorkers) validate() worker.JobHandler { return w.pass() }
func (w *RPTSubmitWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		if _, err := w.submissionID(job); err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		if err := w.complete(context.Background(), client, job, map[string]any{
			"approvalStatus": "APPROVED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}
func (w *RPTSubmitWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		if err := w.complete(context.Background(), client, job, map[string]any{
			"approvalStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *RPTSubmitWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) > 0 {
		withVars, err := cmd.VariablesFromMap(result)
		if err != nil {
			return err
		}
		_, err = withVars.Send(ctx)
		return err
	}
	_, err := cmd.Send(ctx)
	return err
}
