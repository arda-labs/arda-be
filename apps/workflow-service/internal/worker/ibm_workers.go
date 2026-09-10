package worker

import (
	"context"
	"log/slog"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// IBMWorkers run the ibm-place-v1 and ibm-movement-v1 flows (IBM.200/300/301/
// 302/304). The place flow uses a fixed kind; the movement flow reads the kind
// from the case variables (TOP_UP/INTEREST/EXPECTED/WITHDRAW).
type IBMWorkers struct {
	deposit    DepositIBMRequester
	projection *CaseProjection
	kind       string
}

// DepositIBMRequester is the narrow callback surface (deposit gRPC client).
type DepositIBMRequester interface {
	CheckIBMRequest(ctx context.Context, kind, refID string) (bool, string, error)
	ResolveIBMRequest(ctx context.Context, kind, refID, decision, actor string) error
}

func NewIBMWorkers(deposit DepositIBMRequester, caseRepo *repository.CaseRepository, kind string) *IBMWorkers {
	return &IBMWorkers{deposit: deposit, projection: NewCaseProjection(caseRepo), kind: kind}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *IBMWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *IBMWorkers) request(job entities.Job) (kind, refID string, ok bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("ibm worker: invalid variables", "err", err)
		return "", "", false
	}
	kind = w.kind
	if kind == "" {
		kind, _ = vars["kind"].(string)
	}
	kind = strings.ToUpper(kind)
	refID, _ = vars["movementId"].(string)
	if refID == "" {
		refID, _ = vars["depositId"].(string)
	}
	if refID == "" {
		refID, _ = vars["primaryObjectId"].(string)
	}
	if kind == "" || refID == "" {
		slog.Warn("ibm worker: missing kind or ref id", "jobType", job.GetType())
		return "", "", false
	}
	return kind, refID, true
}

func (w *IBMWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		kind, refID, ok := w.request(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		valid, message, err := w.deposit.CheckIBMRequest(crmJobContext(job), kind, refID)
		if err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if !valid {
			throwValidationError(client, job, message)
			return
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("ibm validate complete failed", "jobKey", job.GetKey(), "err", err)
		}
	}
}

func (w *IBMWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		kind, refID, ok := w.request(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		if err := w.deposit.ResolveIBMRequest(crmJobContext(job), kind, refID, "APPROVE", actor); err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *IBMWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		kind, refID, ok := w.request(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		if err := w.deposit.ResolveIBMRequest(crmJobContext(job), kind, refID, "REJECT", actor); err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *IBMWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *IBMWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow ibm job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("ibm fail-job send", "err", err)
	}
}
