package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

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

func (h *WorkflowHandler) tryNativeUserTaskClaim(ctx context.Context, filter service.TaskClaimFilter, elementID, actor string) (*service.WorkflowTask, error) {
	if h.zeebeRest == nil || !h.zeebeRest.Enabled() || filter.ProcessInstanceKey == 0 {
		return nil, nil
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
		if actor != "" {
			if err := h.zeebeRest.AssignUserTask(ctx, ut.UserTaskKey, actor); err != nil {
				slog.Warn("native user task assign failed", "userTaskKey", ut.UserTaskKey, "err", err)
			}
		}
		variables := map[string]any{}
		caseID := filter.CaseID
		if caseID == "" && h.caseRepo != nil {
			if bc, err := h.caseRepo.GetCaseByProcessInstanceKey(ctx, filter.ProcessInstanceKey); err == nil && bc != nil {
				caseID = bc.ID
			}
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
	if err := h.applyNativeUserTaskSideEffects(ctx, elementID, variables, processInstanceKey); err != nil {
		return err
	}
	return h.zeebeRest.CompleteUserTask(ctx, userTaskKey, variables)
}

func (h *WorkflowHandler) applyNativeUserTaskSideEffects(ctx context.Context, elementID string, variables map[string]any, processInstanceKey int64) error {
	if h.crmClient == nil || processInstanceKey == 0 {
		return nil
	}
	decision, _ := variables["reviewDecision"].(string)
	if decision == "" {
		decision, _ = variables["approvalResult"].(string)
	}
	if elementID != "UT_CheckerReview" && elementID != "UT_MakerRevise" {
		return nil
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(ctx, processInstanceKey)
	if err != nil || bc == nil {
		return err
	}
	customerID := bc.PrimaryObjectID
	if customerID == "" {
		return nil
	}
	if elementID == "UT_MakerRevise" {
		return h.crmClient.UpdateCustomerStatus(ctx, customerID, "SUBMITTED")
	}
	if decision == "REQUEST_CHANGES" {
		return h.crmClient.UpdateCustomerStatus(ctx, customerID, "NEEDS_CHANGES")
	}
	return nil
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
