package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// CFC request kinds (mirror capital-service Kind* constants).
const (
	CFCKindFormation = "CONTRACT_FORMATION"
	CFCKindAmendment = "CONTRACT_AMENDMENT"
	CFCKindMovement  = "MOVEMENT"
)

// CFCWorkers run the cfc-contract-v1 / cfc-amendment-v1 / cfc-movement-v1
// flows: validate re-checks the staged object, execute applies the checker
// decision (APPROVE posts), cancel records the rejection. One struct per flow;
// kind selects the domain callback.
type CFCWorkers struct {
	capital    CapitalRequester
	projection *CaseProjection
	kind       string
}

// CapitalRequester is the narrow callback surface (capital gRPC client).
type CapitalRequester interface {
	CheckRequest(ctx context.Context, kind, refID string) (bool, string, error)
	ResolveRequest(ctx context.Context, kind, refID, decision, actor string) error
}

func NewCFCWorkers(capital CapitalRequester, caseRepo *repository.CaseRepository, kind string) *CFCWorkers {
	return &CFCWorkers{capital: capital, projection: NewCaseProjection(caseRepo), kind: kind}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *CFCWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *CFCWorkers) refID(job entities.Job) (string, bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("cfc worker: invalid variables", "err", err, "kind", w.kind)
		return "", false
	}
	for _, key := range []string{"contractId", "amendmentId", "movementId", "primaryObjectId"} {
		if id, _ := vars[key].(string); id != "" {
			return id, true
		}
	}
	slog.Warn("cfc worker: missing staged object id", "jobType", job.GetType(), "kind", w.kind)
	return "", false
}

func (w *CFCWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.refID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		valid, message, err := w.capital.CheckRequest(crmJobContext(job), w.kind, id)
		if err != nil {
			w.failJob(client, job, "Capital Error: "+err.Error())
			return
		}
		if !valid {
			throwValidationError(client, job, message)
			return
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("cfc validate complete failed", "jobKey", job.GetKey(), "err", err)
		}
	}
}

func (w *CFCWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.refID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		if err := w.capital.ResolveRequest(crmJobContext(job), w.kind, id, "APPROVE", actor); err != nil {
			w.failJob(client, job, "Capital Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *CFCWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		id, ok := w.refID(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		if err := w.capital.ResolveRequest(crmJobContext(job), w.kind, id, "REJECT", actor); err != nil {
			w.failJob(client, job, "Capital Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *CFCWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *CFCWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow cfc job failed", "jobType", job.GetType(), "kind", w.kind, "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("cfc fail-job send", "err", err)
	}
}
