package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
)

// CollectionWorkers run the LNM_COLLECTION_V2 flow jobs (lnm-collection-v2.bpmn),
// riding the finance two-phase posting lifecycle like the disbursement legs
// — collection is one case (no register/complete split), so the phases map
// onto the single case:
//   - init:     reserve the cash hold (PENDING entry + available-balance
//     reservation) right after submission, idempotent
//   - validate: loan-service business check, then re-Reserve (rebuilds the
//     hold when the maker edited the receipt)
//   - execute:  post the reserved entry (PENDING → POSTED) — the 4-line
//     LNM_COLLECTION rule card (cash DR principal / CR loan principal /
//     cash DR interest / CR interest receivable) — then settle side effects
//   - cancel:   release the hold (when one exists), reject the receipt
type CollectionWorkers struct {
	loanClient    *loanclient.Client
	financeClient *financeclient.Client
	projection    *CaseProjection
}

func NewCollectionWorkers(loanClient *loanclient.Client, financeClient *financeclient.Client, caseRepo *repository.CaseRepository) *CollectionWorkers {
	return &CollectionWorkers{loanClient: loanClient, financeClient: financeClient, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the init/validate/execute/cancel job handlers.
func (w *CollectionWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.init(), w.validate(), w.execute(), w.cancel()
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

// buildPostingRequest resolves the receipt detail from loan-service and keys
// the hold on the collection id — the same key across init/validate/execute
// is what makes Reserve idempotent and lets Post convert the hold.
// Note (iteration 12 trader stamp): the collection case variables carry no
// trader block today (LNM.301.02 receipts are entered by the maker but the
// posting detail has no trader source yet), so no trader_* metadata is
// stamped here. Wire it via traderStampFromVars when the flow gains a trader
// input.
func (w *CollectionWorkers) buildPostingRequest(ctx context.Context, job entities.Job, id string) (*financev1.PostingRequest, error) {
	detail, err := w.loanClient.GetCollectionPostingDetail(crmJobContext(job), id)
	if err != nil {
		return nil, err
	}
	return &financev1.PostingRequest{
		IdempotencyKey: fmt.Sprintf("lnm-collection-%s", detail.GetCollectionId()),		AccountingDate: detail.GetCollectionDate(),
		CurrencyCode:   detail.GetCurrencyCode(),
		Description:    fmt.Sprintf("Thu nợ %s / %s", detail.GetContractCode(), detail.GetAgreementCode()),
		BusinessReference: &financev1.BusinessReference{
			Domain:       "lnm",
			DocumentType: "LNM_COLLECTION",
			DocumentId:   detail.GetCollectionId(),
			CaseId:       detail.GetWorkflowCaseId(),
		},
		Lines: postingLinesFromRules(fetchPostingRules(w.financeClient, "LNM_COLLECTION"), collectionLegs(detail), detail.GetCurrencyCode()),
	}, nil
}

// collectionLegs builds the rule-card legs (EPAS LNM.301.02). The principal
// pair posts card lines 1-2, the interest pair lines 3-4 — each pair only
// when its amount is positive, so the card rows are referenced explicitly.
// Classifications come from the finance rule card; the constants here are
// only the fallback when a card row is missing.
func collectionLegs(detail *loanv1.CollectionPostingDetail) []postingLeg {
	var legs []postingLeg
	if detail.GetPrincipalMinor() > 0 {
		legs = append(legs,
			postingLeg{
				CardLine:    1,
				Fallback:    "CASH_SETTLEMENT_ACCOUNT",
				Direction:   "DEBIT",
				AmountMinor: detail.GetPrincipalMinor(),
				Analytics: &financev1.Analytics{
					OrgUnitCode:  detail.GetOrgUnitCode(),
					CustomerCode: detail.GetCustomerCode(),
					ContractCode: detail.GetContractCode(),
				},
			},
			postingLeg{
				CardLine:    2,
				Fallback:    "LNM_LOAN_PRINCIPAL",
				Direction:   "CREDIT",
				AmountMinor: detail.GetPrincipalMinor(),
				Analytics: &financev1.Analytics{
					DebtGroupCode: detail.GetDebtGroupCode(),
					OrgUnitCode:   detail.GetOrgUnitCode(),
					CustomerCode:  detail.GetCustomerCode(),
					ContractCode:  detail.GetContractCode(),
				},
			})
	}
	if detail.GetInterestMinor() > 0 {
		legs = append(legs,
			postingLeg{
				CardLine:    3,
				Fallback:    "CASH_SETTLEMENT_ACCOUNT",
				Direction:   "DEBIT",
				AmountMinor: detail.GetInterestMinor(),
				Analytics: &financev1.Analytics{
					OrgUnitCode:  detail.GetOrgUnitCode(),
					CustomerCode: detail.GetCustomerCode(),
					ContractCode: detail.GetContractCode(),
				},
			},
			postingLeg{
				CardLine:    4,
				Fallback:    "LNM_INTEREST_RECEIVABLE",
				Direction:   "CREDIT",
				AmountMinor: detail.GetInterestMinor(),
				Analytics: &financev1.Analytics{
					DebtGroupCode: detail.GetDebtGroupCode(),
					OrgUnitCode:   detail.GetOrgUnitCode(),
					ContractCode:  detail.GetContractCode(),
				},
			})
	}
	return legs
}

// init reserves the posting right after submission — the cash hold exists
// from the moment the case starts (the cash setlement account's available
// balance drops during the approval window). Safe on retries and after maker
// edits: Reserve rebuilds or replays under the same idempotency key.
func (w *CollectionWorkers) init() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCollectionJob("init", job)
		ctx := context.Background()
		id, err := w.collectionID(job)
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
		slog.Info("collection reserved", "id", id, "entry", reserved.GetJournalEntryId(), "status", reserved.GetStatus())
	}
}

// validate re-runs the business check, then re-reserves with the same key —
// if the maker edited the receipt the stale hold is released and rebuilt.
func (w *CollectionWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCollectionJob("validate", job)
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
// receipt side effects (agreement outstanding unwind + journal ref).
func (w *CollectionWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCollectionJob("execute", job)
		ctx := context.Background()
		id, err := w.collectionID(job)
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

// cancel releases the finance hold (when one exists — init may never have
// run) and resolves the receipt as REJECTED.
func (w *CollectionWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCollectionJob("cancel", job)
		ctx := context.Background()
		vars := mustJobVars(job)
		id, _ := vars["collectionId"].(string)
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

func logCollectionJob(phase string, job entities.Job) {
	vars, _ := job.GetVariablesAsMap()
	collectionID, _ := vars["collectionId"].(string)
	slog.Info("loan job", "kind", "collection", "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey(), "collectionId", collectionID)
}

// mustJobVars returns the job variables, tolerating decode failures (empty
// map) for logging/decision paths that already guard on the values.
func mustJobVars(job entities.Job) map[string]any {
	vars, _ := job.GetVariablesAsMap()
	return vars
}
