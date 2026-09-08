package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// DisbursementFlow describes one leg of the two-phase EPAS LNM.300.02
// drawdown. REGISTER reserves the in-transit hold (finance Reserve → Post on
// approve) and COMPLETE settles the cash out of the register.
type DisbursementFlow struct {
	// Flow is the loan disbursement flow_type ("REGISTER" | "COMPLETE").
	Flow string
	// Topic is the BPMN topic prefix, e.g. "lnm.disb-register".
	Topic string
	// IdempotencyPrefix keys the finance posting hold, e.g. "lnm-disb-register".
	IdempotencyPrefix string
	// DocumentType selects the finance accounting rule card.
	DocumentType string
}

// RegisterFlow reserves FUND_DISBURSEMENT_IN_TRANSIT until approval posts it.
var RegisterFlow = DisbursementFlow{
	Flow:              "REGISTER",
	Topic:             "lnm.disb-register",
	IdempotencyPrefix: "lnm-disb-register",
	DocumentType:      "LNM_DISB_REGISTER",
}

// CompleteFlow settles the register's in-transit leg against cash.
var CompleteFlow = DisbursementFlow{
	Flow:              "COMPLETE",
	Topic:             "lnm.disb-complete",
	IdempotencyPrefix: "lnm-disb-complete",
	DocumentType:      "LNM_DISB_COMPLETE",
}

// DisbursementWorkers run the LNM_DISB_REGISTER_V2 / LNM_DISB_COMPLETE_V2
// flow jobs, riding the finance two-phase posting lifecycle:
//   - init:     reserve the posting (PENDING entry + balance hold), idempotent
//   - validate: loan-service business check, then re-reserve (rebuilds the
//     hold when the maker edited the proposal)
//   - execute:  post the reserved entry (PENDING → POSTED), settle the
//     disbursement side effects
//   - cancel:   release the hold, then reject the disbursement
type DisbursementWorkers struct {
	flow          DisbursementFlow
	loanClient    *loanclient.Client
	financeClient *financeclient.Client
	projection    *CaseProjection
}

// NewDisbursementWorkers builds the worker set for one flow leg.
func NewDisbursementWorkers(flow DisbursementFlow, loanClient *loanclient.Client, financeClient *financeclient.Client, caseRepo *repository.CaseRepository) *DisbursementWorkers {
	return &DisbursementWorkers{
		flow:          flow,
		loanClient:    loanClient,
		financeClient: financeClient,
		projection:    NewCaseProjection(caseRepo),
	}
}

// Handlers returns the init/validate/execute/cancel job handlers.
func (w *DisbursementWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.init(), w.validate(), w.execute(), w.cancel()
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

// buildPostingRequest resolves the flow-aware legs from loan-service and
// keys the hold on the disbursement id — the same key across init/validate/
// execute is what makes Reserve idempotent and lets Post convert the hold.
func (w *DisbursementWorkers) buildPostingRequest(ctx context.Context, job entities.Job, id string) (*financev1.PostingRequest, error) {
	detail, err := w.loanClient.GetDisbursementPostingDetail(crmJobContext(job), id)
	if err != nil {
		return nil, err
	}
	return &financev1.PostingRequest{
		IdempotencyKey: fmt.Sprintf("%s-%s", w.flow.IdempotencyPrefix, detail.GetDisbursementId()),
		AccountingDate: detail.GetDisburseDate(),
		CurrencyCode:   detail.GetCurrencyCode(),
		Description:    disbursementDescription(w.flow, detail),
		BusinessReference: &financev1.BusinessReference{
			Domain:       "lnm",
			DocumentType: w.flow.DocumentType,
			DocumentId:   detail.GetDisbursementId(),
			DocumentCode: detail.GetDisbursementCode(),
			CaseId:       detail.GetWorkflowCaseId(),
		},
		Lines: disbursementLines(w.flow, detail),
	}, nil
}

// disbursementLines builds the rule-card legs. REGISTER: DEBIT
// LNM_LOAN_PRINCIPAL / CREDIT FUND_DISBURSEMENT_IN_TRANSIT (LNM.300.02 seq 1).
// COMPLETE: DEBIT FUND_DISBURSEMENT_IN_TRANSIT / CREDIT CASH_SETTLEMENT_ACCOUNT
// (LNM.300.02 seq 2 — reverses the in-transit leg into cash).
func disbursementLines(flow DisbursementFlow, detail *loanv1.DisbursementPostingDetail) []*financev1.PostingLine {
	debitClassification, creditClassification := "LNM_LOAN_PRINCIPAL", "FUND_DISBURSEMENT_IN_TRANSIT"
	if flow.Flow == "COMPLETE" {
		debitClassification, creditClassification = "FUND_DISBURSEMENT_IN_TRANSIT", "CASH_SETTLEMENT_ACCOUNT"
	}
	return []*financev1.PostingLine{
		{
			LineNo:       1,
			Direction:    "DEBIT",
			AmountMinor:  detail.GetDisburseAmtMinor(),
			CurrencyCode: detail.GetCurrencyCode(),
			Analytics: &financev1.Analytics{
				AccClassification: debitClassification,
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
				AccClassification: creditClassification,
				OrgUnitCode:       detail.GetOrgUnitCode(),
				ContractCode:      detail.GetContractCode(),
				FundSourceCode:    detail.GetFundSourceCode(),
			},
		},
	}
}

func disbursementDescription(flow DisbursementFlow, detail *loanv1.DisbursementPostingDetail) string {
	if flow.Flow == "COMPLETE" {
		return fmt.Sprintf("Hoàn tất giải ngân %s / %s", detail.GetContractCode(), detail.GetAgreementCode())
	}
	return fmt.Sprintf("Giải ngân %s / %s", detail.GetContractCode(), detail.GetAgreementCode())
}

// init reserves the posting right after submission — the balance hold exists
// from the moment the case starts. Safe on retries and after maker edits:
// Reserve rebuilds or replays under the same idempotency key.
func (w *DisbursementWorkers) init() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logDisbJob(w.flow, "init", job)
		ctx := context.Background()
		id, err := w.disbursementID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		req, err := w.buildPostingRequest(ctx, job, id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		reserved, err := w.financeClient.Reserve(ctx, req)
		if err != nil {
			w.failJob(client, job, "Posting Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"journalEntryId": reserved.GetJournalEntryId(),
		}); err != nil {
			return
		}
		slog.Info("disbursement reserved", "flow", w.flow.Flow, "id", id, "entry", reserved.GetJournalEntryId(), "status", reserved.GetStatus())
	}
}

// validate re-runs the business check, then re-reserves with the same key —
// if the maker edited the amount the stale hold is released and rebuilt.
func (w *DisbursementWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logDisbJob(w.flow, "validate", job)
		ctx := context.Background()
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
		req, err := w.buildPostingRequest(ctx, job, id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		reserved, err := w.financeClient.Reserve(ctx, req)
		if err != nil {
			w.failJob(client, job, "Posting Error: "+err.Error())
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"journalEntryId": reserved.GetJournalEntryId(),
		}); err != nil {
			return
		}
	}
}

// execute converts the reserved PENDING entry to POSTED, then settles the
// disbursement side effects (register: outstanding + pending; complete:
// pending unwind + contract activation).
func (w *DisbursementWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logDisbJob(w.flow, "execute", job)
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

		req, err := w.buildPostingRequest(ctx, job, id)
		if err != nil {
			w.failJob(client, job, "Loan Error: "+err.Error())
			return
		}
		posted, err := w.financeClient.Post(ctx, req)
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
		slog.Info("disbursement posted", "flow", w.flow.Flow, "id", id, "entry", posted.GetJournalEntryId(), "case", caseID)
	}
}

// cancel releases the finance hold (when one exists — init may never have
// run) and resolves the disbursement as REJECTED.
func (w *DisbursementWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logDisbJob(w.flow, "cancel", job)
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
		// The hold only exists if a reserve step ran; a missing journalEntryId
		// means the case was rejected before any posting was staged.
		if journalEntryID := stringVariable(vars, "journalEntryId"); journalEntryID != "" {
			if _, err := w.financeClient.Release(ctx, &financev1.ReleaseRequest{
				JournalEntryId: journalEntryID,
				Reason:         note,
				Actor:          decidedBy,
			}); err != nil {
				w.failJob(client, job, "Posting Error: "+err.Error())
				return
			}
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
	slog.Warn("workflow disbursement job failed", "flow", w.flow.Flow, "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("disbursement fail-job send", "err", err)
	}
}

func logDisbJob(flow DisbursementFlow, phase string, job entities.Job) {
	vars, _ := job.GetVariablesAsMap()
	disbursementID, _ := vars["disbursementId"].(string)
	slog.Info("loan job", "kind", "disbursement", "flow", flow.Flow, "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey(), "disbursementId", disbursementID)
}
