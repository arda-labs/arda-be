package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// DisbursementWorkers run the LNM_DISBURSEMENT_V2 flow jobs:
//   - validate: loan-service checks the disbursement is actionable
//     (job type lnm.disbursement.validate)
//   - execute:  post LNM_DISBURSEMENT to the finance PostingService, then
//     settle the disbursement (agreement outstanding + POSTED status)
//   - cancel:   reject the disbursement, no posting
type DisbursementWorkers struct {
	loanClient    *loanclient.Client
	financeClient *financeclient.Client
	projection    *CaseProjection
}

func NewDisbursementWorkers(loanClient *loanclient.Client, financeClient *financeclient.Client, caseRepo *repository.CaseRepository) *DisbursementWorkers {
	return &DisbursementWorkers{loanClient: loanClient, financeClient: financeClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *DisbursementWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *DisbursementWorkers) disbursementID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["disbursementId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing disbursementId variable")
	}
	return id, nil
}

func (w *DisbursementWorkers) check(ctx context.Context, job entities.Job, client worker.JobClient) {
	id, err := w.disbursementID(job)
	if err != nil {
		_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
		return
	}
	ok, message, err := w.loanClient.CheckDisbursement(crmJobContext(job), id)
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

func (w *DisbursementWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("disbursement", "validate", job)
		w.check(context.Background(), job, client)
	}
}

func (w *DisbursementWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("disbursement", "execute", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		id, ok := vars["disbursementId"].(string)
		if !ok || id == "" {
			id, _ = vars["primaryObjectId"].(string)
		}
		if id == "" {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		caseID := fmt.Sprintf("%d", job.GetProcessInstanceKey())

		// Resolve posting details from loan-service (amount, currency, agreement).
		detail, err := w.loanClient.GetDisbursementPostingDetail(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}

		postReq := &financev1.PostingRequest{
			IdempotencyKey: fmt.Sprintf("lnm-disbursement-%s", detail.GetDisbursementId()),
			AccountingDate: detail.GetDisburseDate(),
			CurrencyCode:   detail.GetCurrencyCode(),
			Description:    fmt.Sprintf("Giải ngân %s / %s", detail.GetContractCode(), detail.GetAgreementCode()),
			BusinessReference: &financev1.BusinessReference{
				Domain:       "lnm",
				DocumentType: "LNM_DISBURSEMENT",
				DocumentId:   detail.GetDisbursementId(),
				DocumentCode: detail.GetDisbursementCode(),
				CaseId:       detail.GetWorkflowCaseId(),
			},
			Lines: []*financev1.PostingLine{
				{
					LineNo:       1,
					Direction:    "DEBIT",
					AmountMinor:  detail.GetDisburseAmtMinor(),
					CurrencyCode: detail.GetCurrencyCode(),
					Analytics: &financev1.Analytics{
						AccClassification: "LNM_LOAN_PRINCIPAL",
						DebtGroupCode:     detail.GetDebtGroupCode(),
						OrgUnitCode:       detail.GetOrgUnitCode(),
						CustomerCode:      detail.GetCustomerCode(),
						ContractCode:      detail.GetContractCode(),
						Dimensions: map[string]string{
							"agreement_code": detail.GetAgreementCode(),
						},
					},
				},
				{
					LineNo:       2,
					Direction:    "CREDIT",
					AmountMinor:  detail.GetDisburseAmtMinor(),
					CurrencyCode: detail.GetCurrencyCode(),
					Analytics: &financev1.Analytics{
						AccClassification: "FUND_DISBURSEMENT_IN_TRANSIT",
						OrgUnitCode:       detail.GetOrgUnitCode(),
						ContractCode:      detail.GetContractCode(),
						FundSourceCode:    detail.GetFundSourceCode(),
					},
				},
			},
		}
		posted, err := w.financeClient.Post(ctx, postReq)
		if err != nil {
			w.failJob(client, job, "Posting Error: "+err.Error())
			return
		}
		if err := w.loanClient.SettleDisbursement(crmJobContext(job), id, posted.GetJournalEntryId(), actor); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "APPROVED",
			"journalEntryId": posted.GetJournalEntryId(),
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
		slog.Info("disbursement posted", "id", id, "entry", posted.GetJournalEntryId(), "case", caseID)
	}
}

func (w *DisbursementWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("disbursement", "cancel", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		id, _ := vars["disbursementId"].(string)
		if id == "" {
			id, _ = vars["primaryObjectId"].(string)
		}
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if note == "" {
			note = "Rejected by checker"
		}
		if err := w.loanClient.ResolveDisbursement(crmJobContext(job), id, "REJECT", decidedBy, note); err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *DisbursementWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *DisbursementWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow disbursement job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("disbursement fail-job send", "err", err)
	}
}
