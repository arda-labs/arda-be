package handler

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// usesNativeUserTaskRuntime reports whether a task completes through the
// native Zeebe user-task REST path. Registry v2 is the source of truth: a case
// type with ACTIVE registry steps at the case's pinned version runs native
// user tasks (all embedded BPMN processes do; the legacy service-task runtime
// was removed). Cases outside the registry keep the old element-prefix
// behavior so in-flight work is not dropped.
func (h *WorkflowHandler) usesNativeUserTaskRuntime(ctx context.Context, filter service.TaskClaimFilter) bool {
	if !service.IsNativeUserTaskElement(strings.TrimSpace(filter.ElementID)) {
		return false
	}
	bc := h.caseForFilter(ctx, filter)
	if bc == nil || h.caseRepo == nil {
		return true
	}
	version, err := h.caseRepo.CaseRegistryVersion(ctx, bc.ID)
	if err != nil {
		return true
	}
	ok, err := h.caseRepo.CaseTypeHasRegistry(ctx, bc.CaseType, version)
	if err != nil {
		return true
	}
	return ok
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
