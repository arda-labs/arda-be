package handler

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Operate mutations change a running case outside its normal maker/checker
// path, so each one is audited on the case timeline (actor, reason,
// before/after) and the variable edit is restricted to non-protected keys
// inside the scoped process instance. scripts/check-operate-audit.mjs keeps
// every new mutating Operate* handler on recordOperateAudit.

const (
	operateEventVariablesSet       = "OPERATE_VARIABLES_SET"
	operateEventVariablesSetFailed = "OPERATE_VARIABLES_SET_FAILED"
)

// operateEngine is the Zeebe surface used by the audited Operate mutations.
// *service.ZeebeService implements it; tests inject a fake through
// WorkflowHandler.operateEngineOverride.
type operateEngine interface {
	SetVariables(ctx context.Context, elementInstanceKey int64, variables map[string]any, local bool) error
	CancelWorkflow(ctx context.Context, processInstanceKey int64) error
}

// operateReadModel is the exporter read model used to prove an element
// instance belongs to the scoped process instance and to capture the values a
// variable edit overwrites. *service.ZeebeMonitoringIndex implements it.
type operateReadModel interface {
	ListElementInstances(ctx context.Context, processInstanceKey int64) ([]service.ZeebeElementInstance, error)
	ProcessVariables(ctx context.Context, processInstanceKey int64) (map[string]any, error)
}

func (h *WorkflowHandler) operateEngine() operateEngine {
	if h.operateEngineOverride != nil {
		return h.operateEngineOverride
	}
	if h.zeebeSvc == nil {
		return nil
	}
	return h.zeebeSvc
}

func (h *WorkflowHandler) operateReadModel() operateReadModel {
	if h.operateReadModelOverride != nil {
		return h.operateReadModelOverride
	}
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		return nil
	}
	return h.MonitoringIndex
}

// recordOperateAudit writes one Operate audit row on the case timeline through
// the tenant-scoped registry. It fails closed: without the registry no Operate
// mutation can be audited, so none may run.
func (h *WorkflowHandler) recordOperateAudit(ctx context.Context, caseID, eventType, actor, reason string, data map[string]any) error {
	scope := h.caseScope()
	if scope == nil {
		return errWorkflowScopeUnavailable
	}
	return scope.AddOperateAuditEvent(ctx, caseID, eventType, actor, reason, data)
}

// cancelOpenCase moves an open case to CANCELLED with its audit row in one
// transaction (repository.CancelOpenCaseByProcessKey).
func (h *WorkflowHandler) cancelOpenCase(ctx context.Context, processInstanceKey int64, actor, reason string) error {
	scope := h.caseScope()
	if scope == nil {
		return errWorkflowScopeUnavailable
	}
	return scope.CancelOpenCaseByProcessKey(ctx, processInstanceKey, actor, reason)
}

// operateActor is the verified operator; Operate mutations without one are
// rejected because their audit row would have no actor.
func operateActor(r *http.Request) string {
	return firstString(currentUserID(r), strings.TrimSpace(r.Header.Get("X-User-Email")))
}

// elementInstanceInProcess reports whether elementInstanceKey is an element
// instance of processInstanceKey according to the exporter read model.
func elementInstanceInProcess(ctx context.Context, model operateReadModel, processInstanceKey, elementInstanceKey int64) (bool, error) {
	instances, err := model.ListElementInstances(ctx, processInstanceKey)
	if err != nil {
		return false, err
	}
	want := strconv.FormatInt(elementInstanceKey, 10)
	for _, instance := range instances {
		if strings.TrimSpace(instance.ElementInstanceKey) == want {
			return true, nil
		}
	}
	return false, nil
}

// variableNames returns the sorted variable names of an Operate edit.
func variableNames(variables map[string]any) []string {
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isZeebeNotFound reports a cancel on an instance the engine no longer has
// (already completed or cancelled there); the case projection may still be
// cancelled so it does not stay open forever.
func isZeebeNotFound(err error) bool {
	var withStatus interface{ GRPCStatus() *status.Status }
	if errors.As(err, &withStatus) {
		return withStatus.GRPCStatus().Code() == codes.NotFound
	}
	return false
}
