package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

func (h *WorkflowHandler) tryCachedClaimTask(
	ctx context.Context,
	_ string,
	filter service.TaskClaimFilter,
) (*service.WorkflowTask, string, error) {
	if pending, err := h.caseRepo.FindActiveWorkTask(ctx, filter.CaseID, filter.ProcessInstanceKey); err != nil {
		return nil, "", err
	} else if pending != nil && pending.JobKey != nil && *pending.JobKey > 0 {
		task := workItemToWorkflowTask(*pending)
		return &task, "cached_work_task", nil
	}
	return nil, "", nil
}

func claimUnavailableMessage(ctx context.Context, caseRepo *repository.CaseRepository, filter service.TaskClaimFilter, jobType string, lastErr error) string {
	msg := "No task available: " + lastErr.Error()
	bc, _ := caseRepo.GetCase(ctx, filter.CaseID)
	if bc == nil && filter.ProcessInstanceKey > 0 {
		bc, _ = caseRepo.GetCaseByProcessInstanceKey(ctx, filter.ProcessInstanceKey)
	}
	if bc == nil {
		return msg
	}
	step := bc.CurrentStep
	if step == "" || step == "submitted" {
		return msg
	}
	if isUserTaskStep(step) {
		return msg + fmt.Sprintf(" - DB step %q has no native Zeebe user task for job %q; migrate this process to bpmn:userTask.", step, jobType)
	}
	return msg
}

func isUserTaskStep(step string) bool {
	switch step {
	case "Activity_CheckerReview", "Activity_MakerRevise", "Activity_RiskReview",
		"UT_CheckerReview", "UT_MakerRevise":
		return true
	default:
		return strings.HasPrefix(strings.TrimSpace(step), "UT_")
	}
}
