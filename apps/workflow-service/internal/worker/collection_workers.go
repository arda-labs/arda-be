package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
)

// CollectionWorkers run the LNM_COLLECTION_V2 flow jobs. Execute posts the
// 4-line LNM_COLLECTION rule card (cash DR principal / CR loan principal /
// cash DR interest / CR interest receivable) then applies side effects.
type CollectionWorkers struct {
	loanClient    *loanclient.Client
	financeClient *financeclient.Client
	projection    *CaseProjection
}

func NewCollectionWorkers(loanClient *loanclient.Client, financeClient *financeclient.Client, caseRepo *repository.CaseRepository) *CollectionWorkers {
	return &CollectionWorkers{loanClient: loanClient, financeClient: financeClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *CollectionWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.validate(), w.execute(), w.cancel()
}

func (w *CollectionWorkers) collectionID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["collectionId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing collectionId variable")
	}
	return id, nil
}

func (w *CollectionWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("collection", "validate", job)
		ctx := context.Background()
		id, err := w.collectionID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		ok, message, err := w.loanClient.CheckCollection(crmJobContext(job), id)
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

func (w *CollectionWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("collection", "execute", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		id, _ := vars["collectionId"].(string)
		if id == "" {
			id, _ = vars["primaryObjectId"].(string)
		}
		if id == "" {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		actor := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")

		detail, err := w.loanClient.GetCollectionPostingDetail(crmJobContext(job), id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}

		postReq := &financev1.PostingRequest{
			IdempotencyKey: fmt.Sprintf("lnm-collection-%s", detail.GetCollectionId()),
			AccountingDate: detail.GetCollectionDate(),
			CurrencyCode:   detail.GetCurrencyCode(),
			Description:    fmt.Sprintf("Thu nợ %s / %s", detail.GetContractCode(), detail.GetAgreementCode()),
			BusinessReference: &financev1.BusinessReference{
				Domain:       "lnm",
				DocumentType: "LNM_COLLECTION",
				DocumentId:   detail.GetCollectionId(),
				CaseId:       detail.GetWorkflowCaseId(),
			},
		}
		if detail.GetPrincipalMinor() > 0 {
			postReq.Lines = append(postReq.Lines,
				&financev1.PostingLine{
					LineNo:       int32(len(postReq.Lines) + 1),
					Direction:    "DEBIT",
					AmountMinor:  detail.GetPrincipalMinor(),
					CurrencyCode: detail.GetCurrencyCode(),
					Analytics: &financev1.Analytics{
						AccClassification: "CASH_SETTLEMENT_ACCOUNT",
						OrgUnitCode:       detail.GetOrgUnitCode(),
						CustomerCode:      detail.GetCustomerCode(),
						ContractCode:      detail.GetContractCode(),
					},
				},
				&financev1.PostingLine{
					LineNo:       int32(len(postReq.Lines) + 1),
					Direction:    "CREDIT",
					AmountMinor:  detail.GetPrincipalMinor(),
					CurrencyCode: detail.GetCurrencyCode(),
					Analytics: &financev1.Analytics{
						AccClassification: "LNM_LOAN_PRINCIPAL",
						DebtGroupCode:     detail.GetDebtGroupCode(),
						OrgUnitCode:       detail.GetOrgUnitCode(),
						CustomerCode:      detail.GetCustomerCode(),
						ContractCode:      detail.GetContractCode(),
					},
				})
		}
		if detail.GetInterestMinor() > 0 {
			postReq.Lines = append(postReq.Lines,
				&financev1.PostingLine{
					LineNo:       int32(len(postReq.Lines) + 1),
					Direction:    "DEBIT",
					AmountMinor:  detail.GetInterestMinor(),
					CurrencyCode: detail.GetCurrencyCode(),
					Analytics: &financev1.Analytics{
						AccClassification: "CASH_SETTLEMENT_ACCOUNT",
						OrgUnitCode:       detail.GetOrgUnitCode(),
						CustomerCode:      detail.GetCustomerCode(),
						ContractCode:      detail.GetContractCode(),
					},
				},
				&financev1.PostingLine{
					LineNo:       int32(len(postReq.Lines) + 1),
					Direction:    "CREDIT",
					AmountMinor:  detail.GetInterestMinor(),
					CurrencyCode: detail.GetCurrencyCode(),
					Analytics: &financev1.Analytics{
						AccClassification: "LNM_INTEREST_RECEIVABLE",
						DebtGroupCode:     detail.GetDebtGroupCode(),
						OrgUnitCode:       detail.GetOrgUnitCode(),
						ContractCode:      detail.GetContractCode(),
					},
				})
		}

		posted, err := w.financeClient.Post(ctx, postReq)
		if err != nil {
			w.failJob(client, job, "Posting Error: "+err.Error())
			return
		}
		if err := w.loanClient.SettleCollection(crmJobContext(job), id, posted.GetJournalEntryId(), actor); err != nil {
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
		slog.Info("collection posted", "id", id, "entry", posted.GetJournalEntryId())
	}
}

func (w *CollectionWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logLoanJob("collection", "cancel", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		id, _ := vars["collectionId"].(string)
		if id == "" {
			id, _ = vars["primaryObjectId"].(string)
		}
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		note, _ := vars["decisionNote"].(string)
		if note == "" {
			note = "Rejected by checker"
		}
		if err := w.loanClient.ResolveCollection(crmJobContext(job), id, "REJECT", decidedBy, note); err != nil {
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

func (w *CollectionWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *CollectionWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow collection job failed", "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("collection fail-job send", "err", err)
	}
}
