package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// errTaskNotAuthorized marks claim/complete denials so HTTP handlers can map
// them to 403 instead of an upstream failure.
var errTaskNotAuthorized = errors.New("task not authorized")

func normalizeUserTaskElementID(elementID string) string {
	switch strings.TrimSpace(elementID) {
	case "Activity_CheckerReview":
		return "UT_CheckerReview"
	case "Activity_MakerRevise":
		return "UT_MakerRevise"
	default:
		return strings.TrimSpace(elementID)
	}
}

func (h *WorkflowHandler) tryNativeUserTaskClaim(ctx context.Context, r *http.Request, filter service.TaskClaimFilter, elementID, actor string) (*service.WorkflowTask, error) {
	if h.zeebeRest == nil || !h.zeebeRest.Enabled() || filter.ProcessInstanceKey == 0 {
		return nil, nil
	}
	if h.caseRepo == nil {
		return nil, fmt.Errorf("workflow registry is unavailable")
	}
	elementID = normalizeUserTaskElementID(elementID)
	if !service.IsNativeUserTaskElement(elementID) {
		return nil, nil
	}
	tasks, err := h.zeebeRest.SearchUserTasks(ctx, filter.ProcessInstanceKey, "CREATED")
	if err != nil {
		return nil, err
	}
	for _, ut := range tasks {
		if ut.ElementID != elementID {
			continue
		}
		// Authorize against the task's own candidate groups before claiming.
		// The case lookup is tenant-scoped: another tenant's process yields nil.
		bc, err := h.caseRepo.GetCaseByProcessInstanceKey(ctx, ut.ProcessInstanceKey)
		if err != nil {
			return nil, fmt.Errorf("resolve case for task scope: %w", err)
		}
		if bc == nil {
			return nil, fmt.Errorf("%w: task %d does not belong to an accessible case", errTaskNotAuthorized, ut.UserTaskKey)
		}
		if !isSuperadminActor(r) {
			if ut.Assignee != "" && ut.Assignee != actor {
				return nil, fmt.Errorf("%w: task already assigned to another user", errTaskNotAuthorized)
			}
			if ut.Assignee == "" {
				ok, err := h.canClaimCandidateRole(ctx, r, bc.TenantID, firstCandidateGroup(ut.CandidateGroups))
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fmt.Errorf("%w: user is not in the candidate role/group for this task", errTaskNotAuthorized)
				}
			}
		}
		if actor != "" {
			if err := h.zeebeRest.AssignUserTask(ctx, ut.UserTaskKey, actor); err != nil {
				slog.Warn("native user task assign failed", "userTaskKey", ut.UserTaskKey, "err", err)
			}
		}
		variables := map[string]any{}
		caseID := filter.CaseID
		if caseID == "" {
			caseID = bc.ID
		}
		return &service.WorkflowTask{
			JobKey:             ut.UserTaskKey,
			Type:               "zeebe.userTask",
			ElementID:          ut.ElementID,
			ProcessInstanceKey: ut.ProcessInstanceKey,
			CaseID:             caseID,
			CandidateRole:      firstCandidateGroup(ut.CandidateGroups),
			Variables:          variables,
		}, nil
	}
	return nil, nil
}

func (h *WorkflowHandler) completeNativeUserTask(ctx context.Context, userTaskKey int64, elementID string, variables map[string]any, processInstanceKey int64) error {
	if h.zeebeRest == nil || !h.zeebeRest.Enabled() {
		return service.ErrZeebeRestUnavailable
	}
	elementID = normalizeUserTaskElementID(elementID)
	return h.zeebeRest.CompleteUserTask(ctx, userTaskKey, withNormalizedDecision(variables))
}

// withNormalizedDecision makes the BPMN gateway variable (`decision`) the
// single routing input: clients may send reviewDecision/approvalResult, but
// the engine only evaluates `decision`.
func withNormalizedDecision(variables map[string]any) map[string]any {
	if variables == nil {
		return nil
	}
	if decision, _ := variables["decision"].(string); strings.TrimSpace(decision) != "" {
		return variables
	}
	for _, key := range []string{"reviewDecision", "approvalResult"} {
		if value, _ := variables[key].(string); strings.TrimSpace(value) != "" {
			variables["decision"] = value
			return variables
		}
	}
	return variables
}

// recordedDecision returns the decision value stored in the decision log.
// Maker steps complete with a submit action and have no decision variable.
func recordedDecision(elementID string, variables map[string]any) string {
	for _, key := range []string{"decision", "reviewDecision", "approvalResult"} {
		if value, _ := variables[key].(string); strings.TrimSpace(value) != "" {
			return strings.ToUpper(strings.TrimSpace(value))
		}
	}
	if isMakerElement(elementID) {
		return "SUBMIT"
	}
	return "COMPLETE"
}

func isMakerElement(elementID string) bool {
	normalized := normalizeUserTaskElementID(elementID)
	switch normalized {
	case "UT_MakerRevise", "UT_MakerInput", "maker_input", "maker_revise":
		return true
	}
	return strings.HasSuffix(strings.ToLower(normalized), "_maker")
}

func (h *WorkflowHandler) shouldUseNativeUserTaskComplete(ctx context.Context, elementID string, processInstanceKey int64) bool {
	if h.zeebeRest == nil || !h.zeebeRest.Enabled() {
		return false
	}
	normalized := normalizeUserTaskElementID(elementID)
	if !service.IsNativeUserTaskElement(normalized) {
		return false
	}
	if h.caseRepo == nil || processInstanceKey == 0 {
		return false
	}
	return h.usesNativeUserTaskRuntime(ctx, service.TaskClaimFilter{
		ProcessInstanceKey: processInstanceKey,
		ElementID:          elementID,
	})
}

func firstCandidateGroup(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return strings.TrimSpace(groups[0])
}
