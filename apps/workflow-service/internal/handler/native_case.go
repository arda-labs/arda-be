package handler

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

func (h *WorkflowHandler) usesNativeUserTaskRuntime(ctx context.Context, filter service.TaskClaimFilter) bool {
	if service.IsNativeUserTaskElement(strings.TrimSpace(filter.ElementID)) {
		return true
	}
	bc := h.caseForFilter(ctx, filter)
	if bc == nil || bc.BpmnProcessID == nil {
		return false
	}
	return strings.Contains(*bc.BpmnProcessID, "-v2")
}

func (h *WorkflowHandler) caseForFilter(ctx context.Context, filter service.TaskClaimFilter) *repository.BusinessCase {
	if h.caseRepo == nil {
		return nil
	}
	if filter.CaseID != "" {
		bc, _ := h.caseRepo.GetCase(ctx, filter.CaseID)
		return bc
	}
	if filter.ProcessInstanceKey > 0 {
		bc, _ := h.caseRepo.GetCaseByProcessInstanceKey(ctx, filter.ProcessInstanceKey)
		return bc
	}
	return nil
}

func nativeClaimUnavailableMessage(filter service.TaskClaimFilter, cause error) string {
	msg := "Native user task unavailable: " + cause.Error()
	if filter.ProcessInstanceKey > 0 {
		msg += ". v2 processes require ZEEBE_ES_URL (Zeebe Elasticsearch exporter). Running instances started before exporter enablement need a new case."
	}
	return msg
}
