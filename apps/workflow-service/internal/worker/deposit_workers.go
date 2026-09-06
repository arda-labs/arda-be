package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// DepositWorkers run the DPM_SETTLE_V2 flow jobs: validate and settle
// call back into deposit-service (DepositCommandService gRPC surface).
type DepositWorkers struct {
	depositClient DepositSettler
	projection    *CaseProjection
}

// depositClientAlias — the deposit callback client is provided as a narrow
// interface so the worker package stays decoupled from the deposit proto.
type DepositSettler interface {
	CheckSettle(ctx context.Context, savingsCode string) (bool, string, error)
	Settle(ctx context.Context, savingsCode, actor string) error
}

func NewDepositWorkers(depositClient DepositSettler, caseRepo *repository.CaseRepository) *DepositWorkers {
	return &DepositWorkers{depositClient: depositClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *DepositWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *DepositWorkers) savingsCode(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	code, _ := vars["savingsCode"].(string)
	if code == "" {
		code, _ = vars["primaryObjectId"].(string)
	}
	if code == "" {
		return "", fmt.Errorf("missing savingsCode variable")
	}
	return code, nil
}

func (w *DepositWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("dpm-settle", "validate", job)
		ctx := context.Background()
		code, err := w.savingsCode(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		ok, message, err := w.depositClient.CheckSettle(crmJobContext(job), code)
		if err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if !ok {
			throwValidationError(client, job, message)
			return
		}
		_ = w.complete(ctx, client, job, nil)
	}
}

func (w *DepositWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("dpm-settle", "execute", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		code, _ := vars["savingsCode"].(string)
		if code == "" {
			code, _ = vars["primaryObjectId"].(string)
		}
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		if err := w.depositClient.Settle(crmJobContext(job), code, actor); err != nil {
			w.failJob(client, job, "Deposit Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "APPROVED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
		slog.Info("deposit settle done", "savings", code)
	}
}

func (w *DepositWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("dpm-settle", "cancel", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		code, _ := vars["savingsCode"].(string)
		_ = code
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *DepositWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *DepositWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow deposit job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("deposit fail-job send", "err", err)
	}
}
