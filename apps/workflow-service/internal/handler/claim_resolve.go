package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	"github.com/arda-labs/arda/apps/workflow-service/internal/worker"
)

func (h *WorkflowHandler) tryCachedClaimTask(
	ctx context.Context,
	jobType string,
	filter service.TaskClaimFilter,
) (*service.WorkflowTask, string, error) {
	if pending, err := h.caseRepo.FindActiveWorkTask(ctx, filter.CaseID, filter.ProcessInstanceKey); err != nil {
		return nil, "", err
	} else if pending != nil && pending.JobKey != nil && *pending.JobKey > 0 {
		task := workItemToWorkflowTask(*pending)
		return &task, "cached_work_task", nil
	}
	if h.userTaskBroker != nil && !h.usesNativeUserTaskRuntime(ctx, filter) {
		if parked := h.userTaskBroker.FindParked(filter.ProcessInstanceKey, jobType); parked != nil && parkedClaimMatches(filter, *parked) {
			task := parkedUserTaskToWorkflow(*parked)
			h.persistInboxClaim(ctx, task)
			return &task, "parked_user_task", nil
		}
	}
	return nil, "", nil
}

func parkedClaimMatches(filter service.TaskClaimFilter, parked worker.ParkedUserTask) bool {
	if filter.ProcessInstanceKey > 0 && parked.ProcessInstanceKey != filter.ProcessInstanceKey {
		return false
	}
	if filter.CaseID != "" && parked.CaseID != "" && parked.CaseID != filter.CaseID {
		return false
	}
	if filter.ElementID != "" && parked.ElementID != "" && parked.ElementID != filter.ElementID {
		return false
	}
	return true
}

func parkedUserTaskToWorkflow(parked worker.ParkedUserTask) service.WorkflowTask {
	return service.WorkflowTask{
		JobKey:             parked.JobKey,
		Type:               parked.JobType,
		ElementID:          parked.ElementID,
		ProcessInstanceKey: parked.ProcessInstanceKey,
		CaseID:             parked.CaseID,
		CandidateRole:      parked.CandidateRole,
	}
}

func claimUnavailableMessage(ctx context.Context, caseRepo *repository.CaseRepository, broker *worker.UserTaskBroker, filter service.TaskClaimFilter, jobType string, lastErr error) string {
	msg := "No task available: " + lastErr.Error()
	if broker != nil && broker.FindParked(filter.ProcessInstanceKey, jobType) != nil {
		return msg + " — user task worker đang giữ job; thử lại sau vài giây hoặc gọi complete trực tiếp nếu đã có jobKey trên URL."
	}
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
		return msg + fmt.Sprintf(
			" — DB ghi bước %q nhưng Zeebe không có job %q. Case có thể lệch runtime (tạo trước khi restart workflow-service): đăng ký lại hoặc tạo case mới.",
			step, jobType,
		)
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
