package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// ProductRequestWorkers run the dpm-product-register/edit flows (DPM.102/103):
// validate re-checks the staged request, execute applies it (APPROVE) and
// cancel closes it (REJECT). Both flows share the same RPC surface.
type ProductRequestWorkers struct {
	depositClient DepositProductRequester
	projection    *CaseProjection
}

// DepositProductRequester is the narrow callback surface (deposit gRPC client).
type DepositProductRequester interface {
	CheckProductRequest(ctx context.Context, requestID string) (bool, string, error)
	ResolveProductRequest(ctx context.Context, requestID, decision, actor, note string) error
}

func NewProductRequestWorkers(depositClient DepositProductRequester, caseRepo *repository.CaseRepository) *ProductRequestWorkers {
	return &ProductRequestWorkers{depositClient: depositClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *ProductRequestWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *ProductRequestWorkers) requestID(job entities.Job) (string, bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("dpm product request worker: invalid variables", "err", err)
		return "", false
	}
	id, _ := vars["productRequestId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		slog.Warn("dpm product request worker: missing productRequestId", "jobType", job.GetType())
		return "", false
	}
	return id, true
}

func (w *ProductRequestWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.requestID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		valid, message, err := w.depositClient.CheckProductRequest(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if !valid {
			throwValidationError(client, job, message)
			return
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("dpm product request validate complete failed", "jobKey", job.GetKey(), "err", err)
		}
	}
}

func (w *ProductRequestWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.requestID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if err := w.depositClient.ResolveProductRequest(crmJobContext(job), id, "APPROVE", actor, note); err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *ProductRequestWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.requestID(job)
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
		if err := w.depositClient.ResolveProductRequest(crmJobContext(job), id, "REJECT", actor, note); err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *ProductRequestWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *ProductRequestWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow dpm product request job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("dpm product request fail-job send", "err", err)
	}
}
