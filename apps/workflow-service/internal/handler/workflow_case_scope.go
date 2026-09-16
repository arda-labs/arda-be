package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

// errWorkflowScopeUnavailable marks Zeebe-key authorization that cannot run
// because the tenant-scoped case registry is not wired. Fail closed: without a
// registry no Zeebe key can be proven to belong to the caller's tenant.
var errWorkflowScopeUnavailable = errors.New("workflow case registry is unavailable")

// workflowCaseScope is the tenant-scoped case registry used to authorize Zeebe
// keys before any gateway mutation. *repository.CaseRepository implements it in
// production; tests inject a fake through WorkflowHandler.caseScopeOverride.
type workflowCaseScope interface {
	GetCase(ctx context.Context, id string) (*repository.BusinessCase, error)
	GetCaseByProcessInstanceKey(ctx context.Context, processInstanceKey int64) (*repository.BusinessCase, error)
	FindWorkItemByJobKey(ctx context.Context, jobKey int64) (*repository.WorkItem, error)
	SetCaseStatusByProcessKey(ctx context.Context, processInstanceKey int64, status string) error
}

// caseScope returns the registry Zeebe-key authorization runs against, or nil
// when neither the production repository nor a test override is configured.
func (h *WorkflowHandler) caseScope() workflowCaseScope {
	if h.caseScopeOverride != nil {
		return h.caseScopeOverride
	}
	if h.caseRepo == nil {
		return nil
	}
	return h.caseRepo
}

// caseForProcessInstanceKey resolves a Zeebe process instance key to the case
// owned by the caller's verified tenant. (nil, nil) means the key does not map
// to a case in that tenant — including the case where it does not exist at all.
func (h *WorkflowHandler) caseForProcessInstanceKey(ctx context.Context, processInstanceKey int64) (*repository.BusinessCase, error) {
	if processInstanceKey <= 0 {
		return nil, nil
	}
	scope := h.caseScope()
	if scope == nil {
		return nil, errWorkflowScopeUnavailable
	}
	return scope.GetCaseByProcessInstanceKey(ctx, processInstanceKey)
}

// caseForCaseID resolves a case id to the case owned by the caller's verified
// tenant. (nil, nil) means no such case in scope.
func (h *WorkflowHandler) caseForCaseID(ctx context.Context, caseID string) (*repository.BusinessCase, error) {
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return nil, nil
	}
	scope := h.caseScope()
	if scope == nil {
		return nil, errWorkflowScopeUnavailable
	}
	return scope.GetCase(ctx, caseID)
}

// caseForJobKey authorizes a Zeebe job key before a gateway mutation. The
// exporter read model maps job → process instance; the local work-item
// projection (also tenant-scoped) is the fallback when the read model is
// unavailable or has not indexed the job yet. (nil, nil) = not authorized.
func (h *WorkflowHandler) caseForJobKey(ctx context.Context, jobKey int64) (*repository.BusinessCase, error) {
	if jobKey <= 0 {
		return nil, nil
	}
	if h.MonitoringIndex != nil && h.MonitoringIndex.Enabled() {
		job, err := h.MonitoringIndex.GetJob(ctx, jobKey)
		if err != nil {
			return nil, err
		}
		if job != nil {
			processInstanceKey, err := strconv.ParseInt(strings.TrimSpace(job.ProcessInstanceKey), 10, 64)
			if err != nil || processInstanceKey <= 0 {
				return nil, nil
			}
			return h.caseForProcessInstanceKey(ctx, processInstanceKey)
		}
	}
	scope := h.caseScope()
	if scope == nil {
		return nil, errWorkflowScopeUnavailable
	}
	item, err := scope.FindWorkItemByJobKey(ctx, jobKey)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil
	}
	return h.caseForCaseID(ctx, item.CaseID)
}

// caseForIncidentKey authorizes a Zeebe incident key before resolution/retry.
// With the exporter read model the incident must exist and its process instance
// must map to a caller-tenant case; without it the path key only exists in the
// legacy inc-<timelineId> shape, which is authorized as a job key because that
// is the key the degraded retry path actually mutates. (nil, nil) = denied.
func (h *WorkflowHandler) caseForIncidentKey(ctx context.Context, incidentKey int64) (*repository.BusinessCase, error) {
	if incidentKey <= 0 {
		return nil, nil
	}
	if h.MonitoringIndex != nil && h.MonitoringIndex.Enabled() {
		incident, err := h.MonitoringIndex.GetIncident(ctx, incidentKey)
		if err != nil {
			return nil, err
		}
		if incident == nil || incident.ProcessInstanceKey <= 0 {
			return nil, nil
		}
		return h.caseForProcessInstanceKey(ctx, incident.ProcessInstanceKey)
	}
	return h.caseForJobKey(ctx, incidentKey)
}

// setCaseStatusByProcessKey mutates the tenant-scoped case projection behind
// pause/resume/cancel. It keeps those handlers on the injectable registry so
// the mutation cannot run when authorization plumbing is missing.
func (h *WorkflowHandler) setCaseStatusByProcessKey(ctx context.Context, processInstanceKey int64, status string) error {
	if processInstanceKey == 0 {
		return nil
	}
	scope := h.caseScope()
	if scope == nil {
		return errWorkflowScopeUnavailable
	}
	return scope.SetCaseStatusByProcessKey(ctx, processInstanceKey, status)
}

// writeCaseScopeError maps key-authorization failures onto HTTP problems.
// Unmapped keys (nil case) stay at the caller, which renders a 404 with an
// endpoint-specific message so another tenant's data is never disclosed.
func writeCaseScopeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, errWorkflowScopeUnavailable):
		writeAPIError(w, r, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, repository.ErrTenantScopeRequired):
		writeAPIError(w, r, http.StatusForbidden, err.Error())
	case errors.Is(err, repository.ErrNotFound):
		writeAPIError(w, r, http.StatusNotFound, "workflow key does not resolve to a case in the verified tenant")
	default:
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope workflow key: "+err.Error())
	}
}
