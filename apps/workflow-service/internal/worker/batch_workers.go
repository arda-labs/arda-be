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

// BatchFlow describes one leg of the batch (1 hồ sơ — N hợp đồng) LNM flows
// (iteration 13). Mirrors DisbursementFlow/CollectionWorkers: one batch
// dossier per case, one finance PostingRequest carrying N rule-card line
// pairs — Reserve at init, Post on approve, Release on reject.
type BatchFlow struct {
	// BatchType dispatches the loan-service RPCs ("DISB_REGISTER" |
	// "DISB_COMPLETE" | "COLLECTION").
	BatchType string
	// TopicPrefix is the BPMN topic prefix, e.g. "lnm.disb-batch-register".
	TopicPrefix string
	// DocumentType selects the finance accounting rule card.
	DocumentType string
}

// BatchDisbRegisterFlow reserves the in-transit hold for the whole batch.
var BatchDisbRegisterFlow = BatchFlow{
	BatchType:    "DISB_REGISTER",
	TopicPrefix:  "lnm.disb-batch-register",
	DocumentType: "LNM_DISB_REGISTER",
}

// BatchDisbCompleteFlow settles the register batch's in-transit legs
// against cash.
var BatchDisbCompleteFlow = BatchFlow{
	BatchType:    "DISB_COMPLETE",
	TopicPrefix:  "lnm.disb-batch-complete",
	DocumentType: "LNM_DISB_COMPLETE",
}

// BatchCollectionFlow posts the batch receipt (cash DEBIT + principal /
// interest CREDIT pairs per row).
var BatchCollectionFlow = BatchFlow{
	BatchType:    "COLLECTION",
	TopicPrefix:  "lnm.collection-batch",
	DocumentType: "LNM_COLLECTION",
}

// BatchWorkers run the LNM_DISB_BATCH_REGISTER_V2 / LNM_DISB_BATCH_COMPLETE_V2
// / LNM_COLLECTION_BATCH_V2 flow jobs, riding the finance two-phase posting
// lifecycle like the manual posting flows:
//   - init:     reserve the posting (PENDING entry + balance hold), idempotent
//   - validate: loan-service batch check, then re-reserve
//   - execute:  post the reserved entry (PENDING → POSTED), settle the batch
//     side effects row by row in loan-service
//   - cancel:   release the hold, then reject the batch
type BatchWorkers struct {
	flow          BatchFlow
	loanClient    *loanclient.Client
	financeClient *financeclient.Client
	projection    *CaseProjection
}

// NewBatchWorkers builds the worker set for one batch flow leg.
func NewBatchWorkers(flow BatchFlow, loanClient *loanclient.Client, financeClient *financeclient.Client, caseRepo *repository.CaseRepository) *BatchWorkers {
	return &BatchWorkers{
		flow:          flow,
		loanClient:    loanClient,
		financeClient: financeClient,
		projection:    NewCaseProjection(caseRepo),
	}
}

// Handlers returns the init/validate/execute/cancel job handlers.
func (w *BatchWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.init(), w.validate(), w.execute(), w.cancel()
}

func (w *BatchWorkers) batchID(job entities.Job) (string, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", err
	}
	id, _ := vars["batchId"].(string)
	if id == "" {
		id, _ = vars["primaryObjectId"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("missing batchId variable")
	}
	return id, nil
}

// buildPostingRequest resolves the batch detail from loan-service and keys
// the hold on the batch id — the same key across init/validate/execute is
// what makes Reserve idempotent and lets Post convert the hold. The idem-
// potency key prefers the case's postingIdempotencyKey (set at submit) and
// falls back to the prefix + batch id.
func (w *BatchWorkers) buildPostingRequest(ctx context.Context, job entities.Job, id string) (*financev1.PostingRequest, error) {
	vars := mustJobVars(job)
	detail, err := w.loanClient.GetBatchPostingDetail(crmJobContext(job), id, w.flow.BatchType)
	if err != nil {
		return nil, err
	}
	idempotencyKey := stringVariable(vars, "postingIdempotencyKey")
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("%s-%s", w.flow.TopicPrefix, detail.GetBatchId())
	}
	lines := w.batchLines(detail)
	if len(lines) == 0 {
		return nil, fmt.Errorf("batch %s has no positive-amount rows to post", detail.GetBatchId())
	}
	metadata := map[string]string{}
	for k, v := range batchTraderStamp(detail.GetTrader()) {
		metadata[k] = v
	}
	if detail.GetOrgUnitCode() != "" {
		metadata["org_code"] = detail.GetOrgUnitCode()
	}
	req := &financev1.PostingRequest{
		IdempotencyKey: idempotencyKey,
		AccountingDate: detail.GetTxnDate(),
		CurrencyCode:   detail.GetCurrencyCode(),
		Description:    batchDescription(w.flow, detail),
		BusinessReference: &financev1.BusinessReference{
			Domain:       "lnm",
			DocumentType: w.flow.DocumentType,
			DocumentId:   detail.GetBatchId(),
			DocumentCode: detail.GetBatchCode(),
			CaseId:       detail.GetWorkflowCaseId(),
		},
		Lines:    lines,
		Metadata: metadata,
	}
	return req, nil
}

// batchLines builds the rule-card legs: each row contributes one
// DEBIT/CREDIT pair per its document card (LNM_DISB_REGISTER lines 1-2,
// LNM_DISB_COMPLETE lines 1-2, LNM_COLLECTION lines 1-4), skipping zero
// amounts (closed rows carry amount 0). Classifications come from the
// finance rule card; the constants are only the fallback.
func (w *BatchWorkers) batchLines(detail *loanv1.BatchPostingDetail) []*financev1.PostingLine {
	var legs []financeclient.PostingLeg
	for _, row := range detail.GetRows() {
		switch w.flow.BatchType {
		case "COLLECTION":
			legs = append(legs, collectionBatchLegs(detail, row)...)
		case "DISB_COMPLETE":
			legs = append(legs, disbursementBatchLegs("COMPLETE", detail, row)...)
		default:
			legs = append(legs, disbursementBatchLegs("REGISTER", detail, row)...)
		}
	}
	return postingLinesFromRules(fetchPostingRules(w.financeClient, w.flow.DocumentType), legs, detail.GetCurrencyCode())
}

// disbursementBatchLegs builds one row's register/complete pair with the row
// context (contract, agreement dimension, debt group, org unit, customer).
func disbursementBatchLegs(flow string, detail *loanv1.BatchPostingDetail, row *loanv1.BatchRowDetail) []financeclient.PostingLeg {
	debitClassification, creditClassification := "LNM_LOAN_PRINCIPAL", "FUND_DISBURSEMENT_IN_TRANSIT"
	if flow == "COMPLETE" {
		debitClassification, creditClassification = "FUND_DISBURSEMENT_IN_TRANSIT", "CASH_SETTLEMENT_ACCOUNT"
	}
	planDescription := fmt.Sprintf("HĐ %s — %s", row.GetContractCode(), row.GetAgreementCode())
	if row.GetPlanCode() != "" {
		planDescription = fmt.Sprintf("HĐ %s — %s — %s", row.GetContractCode(), row.GetAgreementCode(), row.GetPlanCode())
	}
	return []financeclient.PostingLeg{
		{
			CardLine:    1,
			Fallback:    debitClassification,
			Direction:   "DEBIT",
			AmountMinor: row.GetAmountMinor(),
			Description: planDescription,
			Analytics: &financev1.Analytics{
				DebtGroupCode: row.GetDebtGroupCode(),
				OrgUnitCode:   row.GetOrgUnitCode(),
				CustomerCode:  row.GetCustomerCode(),
				ContractCode:  row.GetContractCode(),
				Dimensions: map[string]string{
					"agreement_code": row.GetAgreementCode(),
					"batch_id":       detail.GetBatchId(),
				},
			},
		},
		{
			CardLine:    2,
			Fallback:    creditClassification,
			Direction:   "CREDIT",
			AmountMinor: row.GetAmountMinor(),
			Description: planDescription,
			Analytics: &financev1.Analytics{
				OrgUnitCode:  row.GetOrgUnitCode(),
				ContractCode: row.GetContractCode(),
				Dimensions: map[string]string{
					"batch_id": detail.GetBatchId(),
				},
			},
		},
	}
}

// collectionBatchLegs builds one receipt row's pairs (EPAS LNM.301.02): the
// principal pair posts card lines 1-2, the interest+overdue pair lines 3-4 —
// each pair only when its amount is positive, so closed/zero rows drop out.
func collectionBatchLegs(detail *loanv1.BatchPostingDetail, row *loanv1.BatchRowDetail) []financeclient.PostingLeg {
	var legs []financeclient.PostingLeg
	planDescription := fmt.Sprintf("HĐ %s — %s", row.GetContractCode(), row.GetAgreementCode())
	if row.GetPlanCode() != "" {
		planDescription = fmt.Sprintf("HĐ %s — %s — %s", row.GetContractCode(), row.GetAgreementCode(), row.GetPlanCode())
	}
	dimensions := map[string]string{
		"agreement_code": row.GetAgreementCode(),
		"batch_id":       detail.GetBatchId(),
	}
	if row.GetPrincipalMinor() > 0 {
		legs = append(legs,
			financeclient.PostingLeg{
				CardLine:    1,
				Fallback:    "CASH_SETTLEMENT_ACCOUNT",
				Direction:   "DEBIT",
				AmountMinor: row.GetPrincipalMinor(),
				Description: planDescription,
				Analytics: &financev1.Analytics{
					OrgUnitCode:  row.GetOrgUnitCode(),
					CustomerCode: row.GetCustomerCode(),
					ContractCode: row.GetContractCode(),
					Dimensions:   dimensions,
				},
			},
			financeclient.PostingLeg{
				CardLine:    2,
				Fallback:    "LNM_LOAN_PRINCIPAL",
				Direction:   "CREDIT",
				AmountMinor: row.GetPrincipalMinor(),
				Description: planDescription,
				Analytics: &financev1.Analytics{
					DebtGroupCode: row.GetDebtGroupCode(),
					OrgUnitCode:   row.GetOrgUnitCode(),
					CustomerCode:  row.GetCustomerCode(),
					ContractCode:  row.GetContractCode(),
					Dimensions:    dimensions,
				},
			})
	}
	// Overdue interest (EPAS 301) rides the interest card pair — the receivable
	// classification is the same LNM_INTEREST_RECEIVABLE leg family.
	if interest := row.GetInterestMinor() + row.GetOverdueInterestMinor(); interest > 0 {
		legs = append(legs,
			financeclient.PostingLeg{
				CardLine:    3,
				Fallback:    "CASH_SETTLEMENT_ACCOUNT",
				Direction:   "DEBIT",
				AmountMinor: interest,
				Description: planDescription,
				Analytics: &financev1.Analytics{
					OrgUnitCode:  row.GetOrgUnitCode(),
					CustomerCode: row.GetCustomerCode(),
					ContractCode: row.GetContractCode(),
					Dimensions:   dimensions,
				},
			},
			financeclient.PostingLeg{
				CardLine:    4,
				Fallback:    "LNM_INTEREST_RECEIVABLE",
				Direction:   "CREDIT",
				AmountMinor: interest,
				Description: planDescription,
				Analytics: &financev1.Analytics{
					DebtGroupCode: row.GetDebtGroupCode(),
					OrgUnitCode:   row.GetOrgUnitCode(),
					ContractCode:  row.GetContractCode(),
					Dimensions:    dimensions,
				},
			})
	}
	return legs
}

// batchDescription is the entry-level description (per-line descriptions
// carry the HĐ <contract> — <plan> detail).
func batchDescription(flow BatchFlow, detail *loanv1.BatchPostingDetail) string {
	switch flow.BatchType {
	case "COLLECTION":
		return fmt.Sprintf("Thu nợ theo hồ sơ %s (%d HĐ)", detail.GetBatchCode(), len(detail.GetRows()))
	case "DISB_COMPLETE":
		return fmt.Sprintf("Hoàn tất giải ngân theo hồ sơ %s (%d HĐ)", detail.GetBatchCode(), len(detail.GetRows()))
	default:
		return fmt.Sprintf("Giải ngân theo hồ sơ %s (%d HĐ)", detail.GetBatchCode(), len(detail.GetRows()))
	}
}

// batchTraderStamp maps the batch's camelCase trader block onto the fixed
// trader_* metadata keys (the same keys the finance journal carries). Absent
// or key-less trader blocks return an empty map — metadata stays org-only.
func batchTraderStamp(trader map[string]string) map[string]string {
	stamp := map[string]string{}
	for varKey, metaKey := range traderStampKeys {
		if v := trader[varKey]; v != "" {
			stamp[metaKey] = v
		}
	}
	return stamp
}

// init reserves the posting right after submission — the balance hold exists
// from the moment the case starts. Safe on retries: Reserve rebuilds or
// replays under the same idempotency key.
func (w *BatchWorkers) init() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logBatchJob(w.flow, "init", job)
		ctx := context.Background()
		id, err := w.batchID(job)
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
		slog.Info("batch reserved", "flow", w.flow.TopicPrefix, "id", id, "entry", reserved.GetJournalEntryId(), "status", reserved.GetStatus())
	}
}

// validate re-runs the batch business check (per-row guards re-validated in
// loan-service), then re-reserves with the same key.
func (w *BatchWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logBatchJob(w.flow, "validate", job)
		ctx := context.Background()
		id, err := w.batchID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		ok, message, err := w.loanClient.CheckBatch(crmJobContext(job), id, w.flow.BatchType)
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
// batch side effects (loan-service loops its per-row settle semantics).
func (w *BatchWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logBatchJob(w.flow, "execute", job)
		ctx := context.Background()
		id, err := w.batchID(job)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		actor := stringVariable(mustJobVars(job), "actorUserId", "actor_user_id", "createdBy", "created_by")

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
		if err := w.loanClient.SettleBatch(crmJobContext(job), id, w.flow.BatchType, posted.GetJournalEntryId(), actor); err != nil {
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
		slog.Info("batch posted", "flow", w.flow.TopicPrefix, "id", id, "entry", posted.GetJournalEntryId())
	}
}

// cancel releases the finance hold (when one exists — init may never have
// run) and resolves the batch as REJECTED. Nothing to unwind: the entry was
// never posted.
func (w *BatchWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logBatchJob(w.flow, "cancel", job)
		ctx := context.Background()
		vars := mustJobVars(job)
		id, _ := vars["batchId"].(string)
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
		if err := w.loanClient.ResolveBatch(crmJobContext(job), id, w.flow.BatchType, "REJECT", decidedBy, note); err != nil {
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

func (w *BatchWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *BatchWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow batch job failed", "flow", w.flow.TopicPrefix, "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("batch fail-job send", "err", err)
	}
}

func logBatchJob(flow BatchFlow, phase string, job entities.Job) {
	vars, _ := job.GetVariablesAsMap()
	batchID, _ := vars["batchId"].(string)
	slog.Info("loan job", "kind", "batch", "flow", flow.TopicPrefix, "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey(), "batchId", batchID)
}
