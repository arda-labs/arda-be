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

// FormationWorkers run the LOAN_FORMATION_V2 jobs (lnm-loan-formation-v2.bpmn,
// EPAS LNM.201.01 — hình thành khoản vay đa cấp). The process is human-flow
// heavy (LNM_MAKER → LNM_TWTD/LNM_POGD → LNM_GIDO/LNM_HODO user tasks); only
// three service tasks need workers:
//
//   - lnm.loan.formation.validate: loan-service checks the contract sits in
//     the submitted PENDING state; a failure throws the BPMN
//     VALIDATION_FAILED boundary error so the case loops back to the maker.
//   - lnm.loan.formation.execute: approve — the contract goes ACTIVE.
//   - lnm.loan.formation.cancel:  reject — the contract goes REJECTED.
//
// EPAS posts no money at formation (the contract is just created; cash only
// moves at the disbursement legs), so these workers deliberately never call
// finance-service.
type FormationWorkers struct {
	loanClient *loanclient.Client
	projection *CaseProjection
}

func NewFormationWorkers(loanClient *loanclient.Client, caseRepo *repository.CaseRepository) *FormationWorkers {
	return &FormationWorkers{loanClient: loanClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *FormationWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *FormationWorkers) contractID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["contractId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing contractId variable")
	}
	return id, nil
}

// validate re-runs the loan-service formation check (contract exists and is
// PENDING). Not ok is a permanent business rejection: the VALIDATION_FAILED
// boundary event returns the case to the maker input instead of burning
// retries.
func (w *FormationWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logFormationJob("validate", job)
		ctx := context.Background()
		id, err := w.contractID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		ok, message, err := w.loanClient.CheckFormation(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if !ok {
			throwValidationError(client, job, message)
			return
		}
		_ = w.complete(ctx, client, job, nil)
	}
}

// execute activates the credit contract (DRAFT → PENDING at submit → ACTIVE
// on approval). UpdateContractStatus is the domain command; no journal entry
// is produced here.
func (w *FormationWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logFormationJob("execute", job)
		ctx := context.Background()
		id, err := w.contractID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		if err := w.loanClient.UpdateContractStatus(crmJobContext(job), id, "ACTIVE"); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "APPROVED",
			"contractStatus": "ACTIVE",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
		slog.Info("loan formation executed", "id", id, "status", "ACTIVE")
	}
}

// cancel rejects the dossier by stamping the contract REJECTED — not DRAFT:
// a rejected dossier is a terminal decision (audit trail), a fresh contract
// is created for a resubmission. Rejected contracts also cannot re-enter
// SubmitContract, which only accepts DRAFT.
func (w *FormationWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logFormationJob("cancel", job)
		ctx := context.Background()
		id, err := w.contractID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		if err := w.loanClient.UpdateContractStatus(crmJobContext(job), id, "REJECTED"); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "REJECTED",
			"contractStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
		slog.Info("loan formation cancelled", "id", id, "status", "REJECTED")
	}
}

func (w *FormationWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *FormationWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow loan formation job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("loan formation fail-job send", "err", err)
	}
}

func logFormationJob(phase string, job entities.Job) {
	vars, _ := job.GetVariablesAsMap()
	contractID, _ := vars["contractId"].(string)
	slog.Info("loan job", "kind", "formation", "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey(), "contractId", contractID)
}
