package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-grpc/client/casecommand"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
)

// CaseCommandCaller is the owner-service side of a shared maker-checker flow
// (arda.common.v1.CaseCommandService, ADR-011).
type CaseCommandCaller interface {
	CheckCase(ctx context.Context, kind, objectID, caseID string) (bool, string, error)
	ResolveCase(ctx context.Context, kind, objectID, caseID string, decision casecommand.Decision, decidedBy, note, dataVersion string) (map[string]string, error)
}

// CaseCommandWorkers are the generic mc.validate|execute|cancel handlers for
// flows whose owner service implements CaseCommandService. They are pure
// transport: every business rule lives in the owner service, and this
// service only completes the job, throws VALIDATION_FAILED for a failed
// check, and projects the case outcome. A new flow on the shared process
// therefore needs no workflow-service code.
type CaseCommandWorkers struct {
	projection *CaseProjection
}

func NewCaseCommandWorkers(caseRepo *repository.CaseRepository) *CaseCommandWorkers {
	return &CaseCommandWorkers{projection: NewCaseProjection(caseRepo)}
}

// Handlers returns the handler triple that routes kind to caller.
func (w *CaseCommandWorkers) Handlers(kind string, caller CaseCommandCaller) MakerCheckerHandlers {
	return MakerCheckerHandlers{
		Validate: w.validate(kind, caller),
		Execute:  w.resolve(kind, caller, casecommand.DecisionApprove),
		Cancel:   w.resolve(kind, caller, casecommand.DecisionReject),
	}
}

type caseJobRef struct {
	objectID    string
	caseID      string
	decidedBy   string
	note        string
	dataVersion string
}

func readCaseJob(job entities.Job) (caseJobRef, error) {
	vars, err := job.GetVariablesAsMap()
	if err != nil {
		return caseJobRef{}, fmt.Errorf("invalid process variables: %w", err)
	}
	ref := caseJobRef{
		objectID:  stringVariable(vars, "primaryObjectId"),
		caseID:    stringVariable(vars, "caseId"),
		decidedBy: stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by"),
		note:      stringVariable(vars, "decisionNote"),
		// The checker form submits the row version it reviewed; legacy cases
		// carry none. Numbers are normalised so the owner compares one format.
		dataVersion: dataVersionVariable(vars),
	}
	if ref.objectID == "" {
		return caseJobRef{}, fmt.Errorf("missing primaryObjectId variable")
	}
	return ref, nil
}

func (w *CaseCommandWorkers) validate(kind string, caller CaseCommandCaller) worker.JobHandler {
	return func(client worker.JobClient, job entities.Job) {
		logCaseCommandJob(kind, "validate", job)
		ref, err := readCaseJob(job)
		if err != nil {
			failCaseCommandJob(client, job, 0, err.Error())
			return
		}
		ok, message, err := caller.CheckCase(crmJobContext(job), kind, ref.objectID, ref.caseID)
		if err != nil {
			failCaseCommandJob(client, job, job.GetRetries()-1, "check case: "+err.Error())
			return
		}
		if !ok {
			if strings.TrimSpace(message) == "" {
				message = "business validation failed"
			}
			w.recordValidationFailure(ref.caseID, message)
			throwValidationError(client, job, message)
			return
		}
		if err := completeCaseCommandJob(client, job, nil); err != nil {
			slog.Error("case command: complete validate job", "kind", kind, "jobKey", job.GetKey(), "err", err)
		}
	}
}

func (w *CaseCommandWorkers) resolve(kind string, caller CaseCommandCaller, decision casecommand.Decision) worker.JobHandler {
	phase, approvalStatus, caseStatus := "execute", "APPROVED", repository.CaseStatusCompleted
	if decision == casecommand.DecisionReject {
		phase, approvalStatus, caseStatus = "cancel", "REJECTED", repository.CaseStatusRejected
	}
	return func(client worker.JobClient, job entities.Job) {
		logCaseCommandJob(kind, phase, job)
		ref, err := readCaseJob(job)
		if err != nil {
			failCaseCommandJob(client, job, 0, err.Error())
			return
		}
		note := ref.note
		if note == "" && decision == casecommand.DecisionReject {
			note = "Rejected by checker"
		}
		outputs, err := caller.ResolveCase(crmJobContext(job), kind, ref.objectID, ref.caseID, decision, ref.decidedBy, note, ref.dataVersion)
		if err != nil {
			failCaseCommandJob(client, job, job.GetRetries()-1, "resolve case: "+err.Error())
			return
		}
		result := map[string]any{}
		for key, value := range outputs {
			if _, reserved := reservedCaseOutputKeys[key]; reserved || strings.TrimSpace(key) == "" {
				slog.Warn("case command: dropped reserved output", "kind", kind, "key", key)
				continue
			}
			result[key] = value
		}
		result["approvalStatus"] = approvalStatus
		if err := completeCaseCommandJob(client, job, result); err != nil {
			slog.Error("case command: complete job", "kind", kind, "phase", phase, "jobKey", job.GetKey(), "err", err)
			return
		}
		if ref.caseID != "" {
			w.projection.FinishCaseByCaseID(context.Background(), ref.caseID, caseStatus)
			return
		}
		w.projection.FinishCase(context.Background(), job.GetProcessInstanceKey(), caseStatus)
	}
}

// recordValidationFailure writes the owner's reason to the case timeline so
// the maker sees why the case returned to input, not only the service log.
func (w *CaseCommandWorkers) recordValidationFailure(caseID, reason string) {
	if w.projection == nil || w.projection.caseRepo == nil || caseID == "" {
		return
	}
	if err := w.projection.caseRepo.AddTimelineEventInternal(context.Background(), caseID, "VALIDATION_FAILED", reason); err != nil {
		slog.Error("case command: record validation failure", "caseId", caseID, "err", err)
	}
}

// dataVersionVariable returns the checker's dataVersion variable as the
// string the owner compares against its row version ("" when absent).
func dataVersionVariable(vars map[string]any) string {
	switch v := vars["dataVersion"].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return ""
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}

// reservedCaseOutputKeys are process variables an owner service must not
// overwrite through ResolveCase outputs.
var reservedCaseOutputKeys = map[string]struct{}{
	"approvalStatus": {}, "decision": {}, "decisionNote": {},
	"caseId": {}, "caseType": {}, "caseCode": {}, "tenantId": {}, "domainService": {},
	"primaryObjectType": {}, "primaryObjectId": {}, "customerId": {},
	"actorUserId": {}, "actor_user_id": {}, "createdBy": {}, "created_by": {},
	"mcKind": {}, "mcMakerRole": {}, "mcCheckerRole": {}, "mcSkipMakerInput": {},
}

func completeCaseCommandJob(client worker.JobClient, job entities.Job, result map[string]any) error {
	cmd := client.NewCompleteJobCommand().JobKey(job.GetKey())
	if len(result) == 0 {
		_, err := cmd.Send(context.Background())
		return err
	}
	withVars, err := cmd.VariablesFromMap(result)
	if err != nil {
		return err
	}
	_, err = withVars.Send(context.Background())
	return err
}

func failCaseCommandJob(client worker.JobClient, job entities.Job, retries int32, reason string) {
	if retries < 0 {
		retries = 0
	}
	slog.Warn("case command job failed", "jobType", job.GetType(), "jobKey", job.GetKey(), "retriesLeft", retries, "reason", reason)
	if _, err := client.NewFailJobCommand().JobKey(job.GetKey()).Retries(retries).ErrorMessage(reason).Send(context.Background()); err != nil {
		slog.Error("case command: fail job", "jobKey", job.GetKey(), "err", err)
	}
}

func logCaseCommandJob(kind, phase string, job entities.Job) {
	slog.Info("case command job", "kind", kind, "phase", phase, "jobType", job.GetType(),
		"jobKey", job.GetKey(), "processInstanceKey", job.GetProcessInstanceKey())
}
