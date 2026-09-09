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

// ManualPostingFlow describes one leg of the FAC-native manual posting flows
// (arda iteration 9). The maker submits accountant-picked lines (account_code
// per line, no classification); the posting rides the finance two-phase
// lifecycle: init reserves the PENDING hold, validate re-checks + re-reserves,
// execute posts on approve, cancel releases the hold on reject.
type ManualPostingFlow struct {
	// TopicPrefix is the BPMN topic prefix, e.g. "fin.single-entry".
	TopicPrefix string
	// IdempotencyPrefix keys the finance posting hold fallback, e.g.
	// "fin-single-entry" (used only when the case carries no posting
	// idempotency key — finance-service always generates one).
	IdempotencyPrefix string
	// DocumentType stamps the finance business_doc_type, e.g. FIN_SINGLE_ENTRY.
	DocumentType string
}

// SingleEntryFlow is the "bút toán lẻ" leg (FIN_SINGLE_ENTRY_V2).
var SingleEntryFlow = ManualPostingFlow{
	TopicPrefix:       "fin.single-entry",
	IdempotencyPrefix: "fin-single-entry",
	DocumentType:      "FIN_SINGLE_ENTRY",
}

// DoubleEntryFlow is the "bút toán kép" leg (FIN_DOUBLE_ENTRY_V2).
var DoubleEntryFlow = ManualPostingFlow{
	TopicPrefix:       "fin.double-entry",
	IdempotencyPrefix: "fin-double-entry",
	DocumentType:      "FIN_DOUBLE_ENTRY",
}

// OffBalanceFlow is the off-balance memo leg (FIN_OFF_BALANCE_V2, iteration
// 10 — nhập xuất ngoại bảng). Pure mirror of the manual posting legs: N
// same-direction equal-amount lines on nature-B accounts (availability-
// exempt in balance_math), Reserve → Validate → Post / Release.
var OffBalanceFlow = ManualPostingFlow{
	TopicPrefix:       "fin.off-balance",
	IdempotencyPrefix: "fin-off-balance",
	DocumentType:      "FIN_OFF_BALANCE",
}

// ClosingFlow is the closing leg (FIN_CLOSING_V2, iteration 11 — kết chuyển
// thu chi FAC.203.01). Pure mirror of the manual posting legs: the case
// variables already carry the server-built postingRequest (finance-service
// constructs the balanced lines from the maker's INC/EXP rows, dest 4211);
// Reserve → Validate → Post / Release. FIN_CLOSING is also the closing-lock
// anchor doc type the finance posting-date policy reads.
var ClosingFlow = ManualPostingFlow{
	TopicPrefix:       "fin.closing",
	IdempotencyPrefix: "fin-closing",
	DocumentType:      "FIN_CLOSING",
}

// postingPolicySentinels are the finance posting-date policy error codes
// (finance service posting_policy_service.go). They arrive inside the gRPC
// error message of Reserve/Post. A policy violation is a proposal problem —
// permanent and maker-fixable — not an infrastructure failure, so the job
// must throw the BPMN VALIDATION_FAILED boundary error (case back to the
// maker) instead of burning job retries.
var postingPolicySentinels = []string{
	"TRANSACTION_DATE_EXCEEDS_CURRENT_DATE",
	"BACKDATE_NOT_ALLOWED",
	"TRANSACTION_DATE_EXCEEDS_BACKDATE",
	"POSTING_DATE_BEFORE_CLOSING_LOCK",
}

// isPostingPolicyError reports whether a finance client failure carries a
// posting-date policy sentinel.
func isPostingPolicyError(err error) bool {
	msg := err.Error()
	for _, sentinel := range postingPolicySentinels {
		if strings.Contains(msg, sentinel) {
			return true
		}
	}
	return false
}

// ManualPostingWorkers run the FIN_SINGLE_ENTRY_V2 / FIN_DOUBLE_ENTRY_V2
// flow jobs, mirroring DisbursementWorkers but sourcing the posting request
// from case variables (the FE-submitted accountant-picked lines) instead of
// a loan-service lookup.
type ManualPostingWorkers struct {
	flow          ManualPostingFlow
	financeClient *financeclient.Client
	projection    *CaseProjection
}

// NewManualPostingWorkers builds the worker set for one manual posting flow.
func NewManualPostingWorkers(flow ManualPostingFlow, financeClient *financeclient.Client, caseRepo *repository.CaseRepository) *ManualPostingWorkers {
	return &ManualPostingWorkers{
		flow:          flow,
		financeClient: financeClient,
		projection:    NewCaseProjection(caseRepo),
	}
}

// Handlers returns the init/validate/execute/cancel job handlers.
func (w *ManualPostingWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.init(), w.validate(), w.execute(), w.cancel()
}

// postingRequestFromVars deserializes the `postingRequest` case variable
// (the camelCase JSON object finance-service submitted) into a finance
// PostingRequest stamped with the flow's document type. The idempotency key
// comes from `postingIdempotencyKey` so init/validate/execute all key the
// same hold; the case id (a SubmitCase base variable) becomes the business
// reference so the ledger row links back to the workflow case.
func postingRequestFromVars(vars map[string]any, flow ManualPostingFlow) (*financev1.PostingRequest, error) {
	idempotencyKey := stringVariable(vars, "postingIdempotencyKey")
	if idempotencyKey == "" {
		return nil, fmt.Errorf("missing postingIdempotencyKey variable")
	}
	raw, ok := vars["postingRequest"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing postingRequest variable")
	}
	req := &financev1.PostingRequest{
		IdempotencyKey: idempotencyKey,
		AccountingDate: stringVariable(raw, "accountingDate"),
		CurrencyCode:   stringVariable(raw, "currencyCode"),
		Description:    stringVariable(raw, "description"),
		BusinessReference: &financev1.BusinessReference{
			Domain:       "fin",
			DocumentType: flow.DocumentType,
			CaseId:       stringVariable(vars, "caseId"),
		},
	}
	// Metadata: the actor stamp serialized by finance-service travels inside
	// the postingRequest variable; the trader_* keys merge from the case's
	// trader block (closing/cancellation variables). Absent on both sides →
	// metadata stays empty.
	if rawMeta, ok := raw["metadata"].(map[string]any); ok {
		req.Metadata = map[string]string{}
		for k, v := range rawMeta {
			if s, ok := v.(string); ok && s != "" {
				req.Metadata[k] = s
			}
		}
	}
	for k, v := range traderStampFromVars(vars) {
		if req.Metadata == nil {
			req.Metadata = map[string]string{}
		}
		req.Metadata[k] = v
	}
	rawLines, ok := raw["lines"].([]any)
	if !ok || len(rawLines) == 0 {
		return nil, fmt.Errorf("postingRequest.lines must not be empty")
	}
	for i, rawLine := range rawLines {
		line, ok := rawLine.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("postingRequest.lines[%d] is not an object", i)
		}
		req.Lines = append(req.Lines, postingLineFromMap(int32(i+1), line))
	}
	return req, nil
}

// postingLineFromMap maps one camelCase line object onto a PostingLine.
// lineNo is positional when the maker did not renumber after edits.
func postingLineFromMap(fallbackLineNo int32, line map[string]any) *financev1.PostingLine {
	l := &financev1.PostingLine{
		LineNo:           fallbackLineNo,
		Direction:        stringVariable(line, "direction"),
		AmountMinor:      intVariable(line, "amountMinor"),
		CurrencyCode:     stringVariable(line, "currencyCode"),
		AccountCode:      stringVariable(line, "accountCode"),
		CoaVersion:       stringVariable(line, "coaVersion"),
		CounterpartyCode: stringVariable(line, "counterpartyCode"),
		Description:      stringVariable(line, "description"),
	}
	if n := intVariable(line, "lineNo"); n > 0 {
		l.LineNo = int32(n)
	}
	return l
}

// intVariable reads an int64 from the JSON number shapes structpb produces.
func intVariable(vars map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := vars[key].(type) {
		case int64:
			return v
		case int32:
			return int64(v)
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return 0
}

// buildPostingRequest deserializes the case variables for the flow. A
// mapping failure is a proposal problem (VALIDATION_FAILED → back to the
// maker), not an infrastructure failure.
func (w *ManualPostingWorkers) buildPostingRequest(vars map[string]any) (*financev1.PostingRequest, error) {
	return postingRequestFromVars(vars, w.flow)
}

// failPostingError routes a finance Reserve/Validate/Post failure to the
// right Zeebe outcome: posting-date policy violations (permanent, maker-
// fixable) throw the VALIDATION_FAILED boundary error so the case returns
// to the maker; everything else is a retryable job failure.
func (w *ManualPostingWorkers) failPostingError(client worker.JobClient, job entities.Job, err error) {
	if isPostingPolicyError(err) {
		throwValidationError(client, job, "Posting Error: "+grpcMessage(err))
		return
	}
	w.failJob(client, job, "Posting Error: "+err.Error())
}

// init reserves the posting right after submission — the balance hold exists
// from the moment the case starts. Safe on retries and after maker edits:
// Reserve rebuilds or replays under the same idempotency key.
func (w *ManualPostingWorkers) init() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logManualPostingJob(w.flow, "init", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		req, err := w.buildPostingRequest(vars)
		if err != nil {
			throwValidationError(client, job, err.Error())
			return
		}
		reserved, err := w.financeClient.Reserve(ctx, req)
		if err != nil {
			w.failPostingError(client, job, err)
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"journalEntryId": reserved.GetJournalEntryId(),
		}); err != nil {
			return
		}
		slog.Info("manual posting reserved", "flow", w.flow.TopicPrefix, "entry", reserved.GetJournalEntryId(), "status", reserved.GetStatus())
	}
}

// validate re-runs the finance validation, then re-reserves with the same
// key — if the maker edited the lines the stale hold is released and rebuilt.
func (w *ManualPostingWorkers) validate() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logManualPostingJob(w.flow, "validate", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		req, err := w.buildPostingRequest(vars)
		if err != nil {
			throwValidationError(client, job, err.Error())
			return
		}
		result, err := w.financeClient.Validate(ctx, req)
		if err != nil {
			w.failJob(client, job, "Posting Error: "+err.Error())
			return
		}
		if !result.GetValid() {
			throwValidationError(client, job, validationErrorsMessage(result))
			return
		}
		reserved, err := w.financeClient.Reserve(ctx, req)
		if err != nil {
			w.failPostingError(client, job, err)
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"journalEntryId": reserved.GetJournalEntryId(),
		}); err != nil {
			return
		}
	}
}

// validationErrorsMessage flattens a failed ValidationResult into one
// readable reason: global errors first, then per-line errors with their
// line number.
func validationErrorsMessage(result *financev1.ValidationResult) string {
	messages := append([]string{}, result.GetGlobalErrors()...)
	for _, l := range result.GetLines() {
		for _, e := range l.GetErrors() {
			messages = append(messages, fmt.Sprintf("line %d: %s", l.GetLineNo(), e))
		}
	}
	if len(messages) == 0 {
		return "posting validation failed"
	}
	return strings.Join(messages, "; ")
}

// execute converts the reserved PENDING entry to POSTED under the same
// idempotency key.
func (w *ManualPostingWorkers) execute() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logManualPostingJob(w.flow, "execute", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
		req, err := w.buildPostingRequest(vars)
		if err != nil {
			_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(ctx)
			return
		}
		posted, err := w.financeClient.Post(ctx, req)
		if err != nil {
			w.failPostingError(client, job, err)
			return
		}
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "APPROVED",
			"journalEntryId": posted.GetJournalEntryId(),
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
		slog.Info("manual posting posted", "flow", w.flow.TopicPrefix, "entry", posted.GetJournalEntryId())
	}
}

// cancel releases the finance hold (when one exists — init may never have
// run) and resolves the case as REJECTED.
func (w *ManualPostingWorkers) cancel() worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logManualPostingJob(w.flow, "cancel", job)
		ctx := context.Background()
		vars, _ := job.GetVariablesAsMap()
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
		if err := w.complete(ctx, client, job, map[string]any{
			"approvalStatus": "REJECTED",
		}); err != nil {
			return
		}
		w.projection.FinishCase(ctx, job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
	}
}

func (w *ManualPostingWorkers) complete(ctx context.Context, client worker.JobClient, job entities.Job, result map[string]any) error {
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

func (w *ManualPostingWorkers) failJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow manual posting job failed", "flow", w.flow.TopicPrefix, "jobType", job.GetType(), "retriesLeft", retries, "reason", reason)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("manual posting fail-job send", "err", err)
	}
}

func logManualPostingJob(flow ManualPostingFlow, phase string, job entities.Job) {
	vars, _ := job.GetVariablesAsMap()
	caseID, _ := vars["caseId"].(string)
	slog.Info("finance job", "kind", "manual-posting", "flow", flow.TopicPrefix, "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey(), "caseId", caseID)
}
