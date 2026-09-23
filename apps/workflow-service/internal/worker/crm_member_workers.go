package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	crmclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/crm"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Job topics for the CRM_MEMBER_V1 flow (QTDND membership capital movements).
const (
	JobCRMMemberValidate = "crm.member.validate"
	JobCRMMemberExecute  = "crm.member.execute"
	JobCRMMemberCancel   = "crm.member.cancel"
)

// CRMMemberWorkers run crm-member-v1: validate re-checks the staged capital
// request against the member's current stake, execute applies the checker
// approval, cancel records the rejection. The domain object lives in
// crm-service and the decision travels back over MemberCommandService gRPC.
type CRMMemberWorkers struct {
	crmClient  MemberRequester
	projection *CaseProjection
}

// MemberRequester is the narrow callback surface (crm gRPC client).
type MemberRequester interface {
	CheckMemberRequest(ctx context.Context, requestID string) (bool, string, error)
	ResolveMemberRequest(ctx context.Context, requestID, decision, actor string, dataVersion int64) error
}

func NewCRMMemberWorkers(client *crmclient.Client, caseRepo *repository.CaseRepository) *CRMMemberWorkers {
	return &CRMMemberWorkers{crmClient: client, projection: NewCaseProjection(caseRepo)}
}

// NewCRMMemberWorkersWithRequester allows a stub requester in tests.
func NewCRMMemberWorkersWithRequester(client MemberRequester, caseRepo *repository.CaseRepository) *CRMMemberWorkers {
	return &CRMMemberWorkers{crmClient: client, projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the validate/execute/cancel job handlers.
func (w *CRMMemberWorkers) Handlers() (worker.JobHandler, worker.JobHandler, worker.JobHandler) {
	return w.ValidateHandler, w.ExecuteHandler, w.CancelHandler
}

func (w *CRMMemberWorkers) ValidateHandler(client worker.JobClient, job entities.Job) {
	requestID, ok := memberRequestID(client, job)
	if !ok {
		return
	}
	valid, message, err := w.crmClient.CheckMemberRequest(crmJobContext(job), requestID)
	if err != nil {
		failWorkflowJob(client, job, "CRM Error: "+err.Error())
		return
	}
	if !valid {
		throwValidationError(client, job, message)
		return
	}
	if err := completeWorkflowJob(client, job, nil); err != nil {
		slog.Error("crm member validate complete failed", "jobKey", job.GetKey(), "err", err)
	}
}

func (w *CRMMemberWorkers) ExecuteHandler(client worker.JobClient, job entities.Job) {
	requestID, ok := memberRequestID(client, job)
	if !ok {
		return
	}
	actor, dataVersion := memberDecisionContext(job)
	if err := w.crmClient.ResolveMemberRequest(crmJobContext(job), requestID, "APPROVE", actor, dataVersion); err != nil {
		failMemberDecision(client, job, err)
		return
	}
	if err := completeWorkflowJob(client, job, map[string]any{"approvalStatus": "APPROVED"}); err != nil {
		return
	}
	w.projection.AfterServiceTaskCompleted(context.Background(), job.GetProcessInstanceKey(), "ST_Execute", "")
	w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusCompleted)
}

func (w *CRMMemberWorkers) CancelHandler(client worker.JobClient, job entities.Job) {
	requestID, ok := memberRequestID(client, job)
	if !ok {
		return
	}
	actor, dataVersion := memberDecisionContext(job)
	if err := w.crmClient.ResolveMemberRequest(crmJobContext(job), requestID, "REJECT", actor, dataVersion); err != nil {
		failMemberDecision(client, job, err)
		return
	}
	if err := completeWorkflowJob(client, job, map[string]any{"approvalStatus": "REJECTED"}); err != nil {
		return
	}
	w.projection.AfterServiceTaskCompleted(context.Background(), job.GetProcessInstanceKey(), "ST_Cancel", "")
	w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), repository.CaseStatusRejected)
}

// memberRequestID reads the staged request id from the case variables.
func memberRequestID(client worker.JobClient, job entities.Job) (string, bool) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		slog.Warn("crm member worker: invalid variables", "err", err)
		return "", false
	}
	for _, key := range []string{"request_id", "requestId", "primaryObjectId"} {
		if id, _ := vars[key].(string); id != "" {
			return id, true
		}
	}
	slog.Warn("crm member worker: missing request id", "jobType", job.GetType())
	_, _ = client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).Send(context.Background())
	return "", false
}

// memberDecisionContext reads the checker actor and the optimistic-concurrency
// token the checker saw.
func memberDecisionContext(job entities.Job) (string, int64) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return "", 0
	}
	actor, _ := vars["actor"].(string)
	if actor == "" {
		actor, _ = vars["decisionBy"].(string)
	}
	var version int64
	switch v := vars["dataVersion"].(type) {
	case float64:
		version = int64(v)
	case int64:
		version = v
	case int:
		version = int64(v)
	}
	return actor, version
}

// completeWorkflowJob completes a job with optional result variables.
func completeWorkflowJob(client worker.JobClient, job entities.Job, result map[string]any) error {
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

// failWorkflowJob fails a job with bounded retries (a domain callback outage
// should be retried, not dropped). The retry budget is decremented from the
// job's own counter — a fixed value would reset it on every attempt and spin
// forever.
func failWorkflowJob(client worker.JobClient, job entities.Job, reason string) {
	retries := job.GetRetries() - 1
	if retries < 0 {
		retries = 0
	}
	slog.Warn("workflow crm member job failed",
		"jobKey", job.GetKey(),
		"jobType", job.GetType(),
		"retriesLeft", retries,
		"reason", reason,
	)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("fail job", "jobKey", job.GetKey(), "err", err)
	}
}

// failMemberDecision routes a ResolveMemberRequest error: a version/status
// conflict is terminal (the token the checker saw is stale, so retrying with
// the same value can never succeed) and stops the job with an incident for
// ops; anything else is retried with the remaining budget.
func failMemberDecision(client worker.JobClient, job entities.Job, err error) {
	if isMemberTerminalError(err) {
		failWorkflowJobTerminal(client, job, "CRM Conflict: "+status.Convert(err).Message())
		return
	}
	failWorkflowJob(client, job, "CRM Error: "+err.Error())
}

// isMemberTerminalError reports whether a ResolveMemberRequest failure is a
// permanent domain conflict. Aborted is the stale optimistic-lock token
// (repository.ErrMemberVersionConflict); FailedPrecondition is a request that
// cannot be decided (already decided / not found / malformed decision).
func isMemberTerminalError(err error) bool {
	switch status.Code(err) {
	case codes.Aborted, codes.FailedPrecondition:
		return true
	default:
		return false
	}
}

// failWorkflowJobTerminal stops a job without retries when retrying cannot
// change the outcome (stale data version / status conflict).
func failWorkflowJobTerminal(client worker.JobClient, job entities.Job, reason string) {
	slog.Warn("workflow crm member job stopped",
		"jobKey", job.GetKey(),
		"jobType", job.GetType(),
		"reason", reason,
	)
	_, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(0).ErrorMessage(reason).Send(context.Background())
	if err != nil {
		slog.Error("fail job terminal", "jobKey", job.GetKey(), "err", err)
	}
}
