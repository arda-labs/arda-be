package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// CancellationFlow describes the FIN_TXN_CANCEL_V2 leg (arda iteration 10 —
// hủy giao dịch). Unlike the manual posting legs there is no posting
// request: the case references one POSTED journal entry (by human entry_no)
// and the execute step reverses it on checker approval. No Reserve/Release —
// nothing is held, the cancel path only resolves the case as REJECTED.
type CancellationFlow struct {
	// TopicPrefix is the BPMN topic prefix, e.g. "fin.txn-cancel".
	TopicPrefix string
	// IdempotencyPrefix keys the reversal fallback, e.g. "fin-txn-cancel"
	// (finance-service always generates one; the suffix "-reverse" keeps the
	// reversal key disjoint from the case-create key).
	IdempotencyPrefix string
	// DocumentType stamps the reversal entry, e.g. FIN_TXN_CANCEL (the
	// journal list filters cancel-driven reversals on it).
	DocumentType string
}

// TxnCancelFlow is the transaction cancellation leg (FIN_TXN_CANCEL_V2).
var TxnCancelFlow = CancellationFlow{
	TopicPrefix:       "fin.txn-cancel",
	IdempotencyPrefix: "fin-txn-cancel",
	DocumentType:      "FIN_TXN_CANCEL",
}

// CancellationWorkers run the FIN_TXN_CANCEL_V2 flow jobs: init/validate
// read the original entry through financeClient.GetJournalEntry (guarding
// status POSTED and not-yet-reversed), execute reverses it with the flow's
// document type.
type CancellationWorkers struct {
	flow          CancellationFlow
	financeClient *financeclient.Client
	projection    *CaseProjection
}

// NewCancellationWorkers builds the worker set for the cancellation flow.
func NewCancellationWorkers(flow CancellationFlow, financeClient *financeclient.Client, caseRepo *repository.CaseRepository) *CancellationWorkers {
	return &CancellationWorkers{
		flow:          flow,
		financeClient: financeClient,
		projection:    NewCaseProjection(caseRepo),
	}
}

// Handlers returns the init/validate/execute/cancel job handlers.
func (w *CancellationWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.init(), w.validate(), w.execute(), w.cancel()
}

// cancellationRequestFromVars deserializes the `cancellationRequest` case
// variable (the camelCase JSON object finance-service submitted). The
// idempotency key falls back to `postingIdempotencyKey` — the create+submit
// key finance-service generated for the whole case.
func cancellationRequestFromVars(vars map[string]any) (lookup *financev1.GetJournalEntryRequest, reason, accountingDate, idempotencyKey string, err error) {
	raw, ok := vars["cancellationRequest"].(map[string]any)
	if !ok {
		return nil, "", "", "", fmt.Errorf("missing cancellationRequest variable")
	}
	referenceEntryNo := stringVariable(raw, "referenceEntryNo")
	if referenceEntryNo == "" {
		return nil, "", "", "", fmt.Errorf("cancellationRequest.referenceEntryNo must not be empty")
	}
	reason = stringVariable(raw, "reason")
	if reason == "" {
		return nil, "", "", "", fmt.Errorf("cancellationRequest.reason must not be empty")
	}
	idempotencyKey = stringVariable(raw, "idempotencyKey")
	if idempotencyKey == "" {
		idempotencyKey = stringVariable(vars, "postingIdempotencyKey")
	}
	if idempotencyKey == "" {
		// Defensive last resort: finance-service always generates a key.
		idempotencyKey = "fin-txn-cancel-" + stringVariable(vars, "caseId", "case_id")
	}
	return &financev1.GetJournalEntryRequest{
		TenantId: stringVariable(vars, "tenantId", "tenant_id"),
		EntryNo:  referenceEntryNo,
	}, reason, stringVariable(raw, "accountingDate"), idempotencyKey, nil
}

// guardOriginalEntry enforces the reversal preconditions on the read entry:
// it must exist, be POSTED (PENDING/VOID report not-found from finance) and
// not already reversed.
func guardOriginalEntry(entry *financev1.JournalEntryDetail) error {
	if entry.GetStatus() != "POSTED" {
		return fmt.Errorf("original entry %d has status %s; only POSTED entries can be cancelled", entry.GetEntryNo(), entry.GetStatus())
	}
	if entry.GetReversedByEntryId() != "" {
		return fmt.Errorf("original entry %d was already reversed", entry.GetEntryNo())
	}
	if len(entry.GetLines()) == 0 {
		return fmt.Errorf("original entry %d has no lines", entry.GetEntryNo())
	}
	return nil
}

// originalEntryVars projects the read entry onto the case variables the FE
// popup shows (original number, amount, status).
func originalEntryVars(entry *financev1.JournalEntryDetail) map[string]any {
	return map[string]any{
		"journalEntryId":           entry.GetJournalEntryId(),
		"originalEntryNo":          entry.GetEntryNo(),
		"originalStatus":           entry.GetStatus(),
		"originalTotalAmountMinor": entry.GetTotalAmountMinor(),
	}
}

// init resolves the referenced original entry right after submission so the
// maker sees the real entry (number / amount / status) on the form. A
// lookup failure or a guard violation is a proposal problem
// (VALIDATION_FAILED → back to the maker), not an infrastructure failure.
func (w *CancellationWorkers) init() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCancellationJob(w.flow, "init", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		lookup, _, _, _, err := cancellationRequestFromVars(vars)
		if err != nil {
			throwValidationError(client, job, err.Error())
			return
		}
		entry, err := w.financeClient.GetJournalEntry(crmJobContext(job), lookup)
		if err != nil {
			throwValidationError(client, job, "Original Entry Error: "+grpcMessage(err))
			return
		}
		if err := guardOriginalEntry(entry); err != nil {
			throwValidationError(client, job, err.Error())
			return
		}
		if err := w.complete(ctx, client, job, originalEntryVars(entry)); err != nil {
			return
		}
		slog.Info("cancellation resolved original", "flow", w.flow.TopicPrefix, "entry", entry.GetJournalEntryId(), "status", entry.GetStatus())
	}
}

// validate re-checks the original right before the checker review — it may
// have been reversed (or voided) by another path while the case sat open.
func (w *CancellationWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCancellationJob(w.flow, "validate", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		lookup, _, _, _, err := cancellationRequestFromVars(vars)
		if err != nil {
			throwValidationError(client, job, err.Error())
			return
		}
		entry, err := w.financeClient.GetJournalEntry(crmJobContext(job), lookup)
		if err != nil {
			throwValidationError(client, job, "Original Entry Error: "+grpcMessage(err))
			return
		}
		if err := guardOriginalEntry(entry); err != nil {
			throwValidationError(client, job, err.Error())
			return
		}
		if err := w.complete(ctx, client, job, originalEntryVars(entry)); err != nil {
			return
		}
	}
}

// execute reverses the original under the flow's document type (the
// reversal entry is stamped FIN_TXN_CANCEL so the journal list can filter
// it), then resolves the case APPROVED/COMPLETED. The reversal idempotency
// key is the case key suffixed "-reverse" — disjoint from every posting
// key, stable across job retries.
func (w *CancellationWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCancellationJob(w.flow, "execute", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		lookup, reason, accountingDate, idempotencyKey, err := cancellationRequestFromVars(vars)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		journalEntryID := stringVariable(vars, "journalEntryId")
		if journalEntryID == "" {
			throwValidationError(client, job, "missing journalEntryId variable (init never ran)")
			return
		}
		decidedBy := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
		reversed, err := w.financeClient.Reverse(crmJobContext(job), &financev1.ReverseRequest{
			TenantId:             lookup.GetTenantId(),
			JournalEntryId:       journalEntryID,
			BusinessDocumentType: w.flow.DocumentType,
			Reason:               reason,
			AccountingDate:       accountingDate,
			IdempotencyKey:       idempotencyKey + "-reverse",
			Actor:                decidedBy,
		})
		if err != nil {
			w.failJob(client, job, "Posting Error: "+grpcMessage(err))
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus":  "APPROVED",
			"reversalEntryId": reversed.GetJournalEntryId(),
			"reversalEntryNo": reversed.GetEntryNo(),
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
		slog.Info("cancellation reversed original", "flow", w.flow.TopicPrefix, "original", journalEntryID, "reversal", reversed.GetJournalEntryId())
	}
}

// cancel resolves the case REJECTED. No finance hold was ever reserved for a
// cancellation, so there is nothing to Release — unlike the manual posting
// legs.
func (w *CancellationWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCancellationJob(w.flow, "cancel", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		note, _ := vars["decisionNote"].(string)
		if note == "" {
			note = "Rejected by checker"
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

// grpcMessage flattens a gRPC error to its message for job failures.
func grpcMessage(err error) string {
	msg := err.Error()
	if idx := strings.LastIndex(msg, "desc = "); idx >= 0 {
		msg = msg[idx+len("desc = "):]
	}
	return msg
}

func (w *CancellationWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *CancellationWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow cancellation job failed", "flow", w.flow.TopicPrefix, "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("cancellation fail-job send", "err", err)
	}
}

func logCancellationJob(flow CancellationFlow, phase string, job entities.Job) {
	vars, _ := job.GetVariablesAsMap()
	caseID, _ := vars["caseId"].(string)
	slog.Info("finance job", "kind", "txn-cancel", "flow", flow.TopicPrefix, "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey(), "caseId", caseID)
}
