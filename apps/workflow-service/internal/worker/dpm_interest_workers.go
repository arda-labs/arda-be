package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// DPM interest/rate worker modes.
const (
	DPMInterestModeRate  = "RATE"
	DPMInterestModeOp    = "OP"
	DPMInterestModeBatch = "BATCH"
)

// DPMInterestWorkers run the dpm-rate-v1 and dpm-interest-v1 flows
// (DPM.100/101 rates; 302/303 single ops; 304 batch over opIds).
type DPMInterestWorkers struct {
	deposit    DepositInterestRequester
	projection *CaseProjection
	mode       string
}

// DepositInterestRequester is the narrow callback surface (deposit gRPC client).
type DepositInterestRequester interface {
	CheckRateRequest(ctx context.Context, requestID string) (bool, string, error)
	ResolveRateRequest(ctx context.Context, requestID, decision, actor string) error
	CheckInterestOp(ctx context.Context, opID string) (bool, string, error)
	ResolveInterestOp(ctx context.Context, opID, decision, actor string) error
}

func NewDPMInterestWorkers(deposit DepositInterestRequester, caseRepo *repository.CaseRepository, mode string) *DPMInterestWorkers {
	return &DPMInterestWorkers{deposit: deposit, projection: NewCaseProjection(caseRepo), mode: mode}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *DPMInterestWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *DPMInterestWorkers) opIDs(job entities.Job) ([]string, bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("dpm interest worker: invalid variables", "err", err)
		return nil, false
	}
	switch w.mode {
	case DPMInterestModeRate:
		if id, _ := vars["requestId"].(string); id != "" {
			return []string{id}, true
		}
	case DPMInterestModeOp:
		if id, _ := vars["opId"].(string); id != "" {
			return []string{id}, true
		}
		// Batch cases share the interest topics; the op list arrives as opIds.
		raw, _ := vars["opIds"].([]any)
		ids := make([]string, 0, len(raw))
		for _, item := range raw {
			if id, _ := item.(string); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			return ids, true
		}
	case DPMInterestModeBatch:
		raw, _ := vars["opIds"].([]any)
		ids := make([]string, 0, len(raw))
		for _, item := range raw {
			if id, _ := item.(string); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			return ids, true
		}
	}
	slog.Warn("dpm interest worker: missing ids", "jobType", job.GetType(), "mode", w.mode)
	return nil, false
}

func (w *DPMInterestWorkers) validateOne(ctx context.Context, id string) (bool, string, error) {
	if w.mode == DPMInterestModeRate {
		return w.deposit.CheckRateRequest(ctx, id)
	}
	return w.deposit.CheckInterestOp(ctx, id)
}

func (w *DPMInterestWorkers) resolveOne(ctx context.Context, id, decision, actor string) error {
	if w.mode == DPMInterestModeRate {
		return w.deposit.ResolveRateRequest(ctx, id, decision, actor)
	}
	return w.deposit.ResolveInterestOp(ctx, id, decision, actor)
}

func (w *DPMInterestWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		ids, ok := w.opIDs(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		for _, id := range ids {
			valid, message, err := w.validateOne(crmJobContext(job), id)
			if err != nil {
				w.failJob(client, job, "Deposit Error: "+err.Error())
				return
			}
			if !valid {
				throwValidationError(client, job, message)
				return
			}
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("dpm interest validate complete failed", "jobKey", job.GetKey(), "err", err)
		}
	}
}

func (w *DPMInterestWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		ids, ok := w.opIDs(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		for _, id := range ids {
			if err := w.resolveOne(crmJobContext(job), id, "APPROVE", actor); err != nil {
				w.failJob(client, job, "Deposit Error: "+err.Error())
				return
			}
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *DPMInterestWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		ids, ok := w.opIDs(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		for _, id := range ids {
			if err := w.resolveOne(crmJobContext(job), id, "REJECT", actor); err != nil {
				w.failJob(client, job, "Deposit Error: "+err.Error())
				return
			}
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *DPMInterestWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *DPMInterestWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow dpm interest job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("dpm interest fail-job send", "err", err)
	}
}
