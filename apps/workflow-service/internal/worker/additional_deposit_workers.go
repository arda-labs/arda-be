package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// AdditionalDepositWorkers run the dpm-additional-v1 flow jobs (DPM.301):
// validate re-checks the savings, execute posts the movement + bumps the
// principal through deposit-service, cancel just closes the case (nothing is
// reserved before posting).
type AdditionalDepositWorkers struct {
	depositClient DepositAdditionaler
	projection    *CaseProjection
}

// DepositAdditionaler is the narrow callback surface (deposit gRPC client).
type DepositAdditionaler interface {
	CheckAdditional(ctx context.Context, savingsCode string, amountMinor int64) (bool, string, error)
	SettleAdditional(ctx context.Context, savingsCode string, amountMinor int64, txnDate, idempotencyKey, actor string) error
}

func NewAdditionalDepositWorkers(depositClient DepositAdditionaler, caseRepo *repository.CaseRepository) *AdditionalDepositWorkers {
	return &AdditionalDepositWorkers{depositClient: depositClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *AdditionalDepositWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *AdditionalDepositWorkers) variables(job entities.Job) (string, int64, string, string, bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("dpm additional worker: invalid variables", "err", err)
		return "", 0, "", "", false
	}
	code, _ := vars["savingsCode"].(string)
	if code == "" {
		code, _ = vars["primaryObjectId"].(string)
	}
	amount, _ := vars["amountMinor"].(float64)
	txnDate, _ := vars["txnDate"].(string)
	idemKey, _ := vars["idempotencyKey"].(string)
	if code == "" || amount <= 0 {
		slog.Warn("dpm additional worker: missing savingsCode/amountMinor", "jobType", job.GetType())
		return "", 0, "", "", false
	}
	return code, int64(amount), txnDate, idemKey, true
}

func (w *AdditionalDepositWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		code, amount, _, _, ok := w.variables(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		valid, message, err := w.depositClient.CheckAdditional(crmJobContext(job), code, amount)
		if err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if !valid {
			throwValidationError(client, job, message)
			return
		}
		if err := w.completeJob(client, job, nil); err != nil {
			slog.Error("dpm additional validate complete failed", "jobKey", job.GetKey(), "err", err)
		}
	}
}

func (w *AdditionalDepositWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		code, amount, txnDate, idemKey, ok := w.variables(job)
		if !ok {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
			return
		}
		vars, _ := job.GetVariablesAsMap()
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		if err := w.depositClient.SettleAdditional(crmJobContext(job), code, amount, txnDate, idemKey, actor); err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *AdditionalDepositWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		if err := w.completeJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *AdditionalDepositWorkers) completeJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *AdditionalDepositWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow dpm additional job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("dpm additional fail-job send", "err", err)
	}
}
