package handler

import (
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// Named request and result types of the public workflow REST surface. They
// replace the map[string]any and anonymous-struct payloads the handlers used
// to write or decode, so contracts/openapi/workflow-v1.json (generated from
// internal/transport/http/openapi.go, ADR-005) carries the exact wire shape.
// The JSON on the wire is unchanged; camelCase keys are the legacy workflow
// contract tracked in scripts/check-json-tags.mjs.

// StatusResult is the {status} acknowledgement that commands answer with.
type StatusResult struct {
	Status string `json:"status"`
}

// DeployResult answers POST /api/workflow/deploy.
type DeployResult struct {
	ProcessDefinitionKey int64  `json:"processDefinitionKey"`
	Filename             string `json:"filename"`
	Status               string `json:"status"`
}

// StartResult answers POST /api/workflow/start.
type StartResult struct {
	ProcessInstanceKey int64  `json:"processInstanceKey"`
	BusinessKey        string `json:"businessKey"`
	Status             string `json:"status"`
}

// CancelResult answers POST /api/workflow/instances/{key}/cancel.
type CancelResult struct {
	ProcessInstanceKey int64  `json:"processInstanceKey"`
	Status             string `json:"status"`
}

// CaseTypeSteps answers GET /api/workflow/case-types/{caseType}/steps.
type CaseTypeSteps struct {
	CaseType        string                    `json:"caseType"`
	RegistryVersion int                       `json:"registryVersion"`
	Steps           []repository.CaseTypeStep `json:"steps"`
}

// CompleteUserTaskRequest is the body of POST /api/workflow/tasks/{jobKey}/complete.
type CompleteUserTaskRequest struct {
	ProcessInstanceKey flexInt64      `json:"processInstanceKey"`
	ElementID          string         `json:"elementId"`
	Variables          map[string]any `json:"variables"`
}

// ClaimTaskRequest is the body of POST /api/workflow/tasks/claim.
type ClaimTaskRequest struct {
	Role               string    `json:"role"`
	TaskType           string    `json:"taskType"`
	ProcessInstanceKey flexInt64 `json:"processInstanceKey"`
	CaseID             string    `json:"caseId"`
	ElementID          string    `json:"elementId"`
}

// WorkItemSummaryResult answers GET /api/workflow/work-items/summary.
type WorkItemSummaryResult struct {
	Nodes []repository.WorkItemSummaryNode `json:"nodes"`
}

// WorkItemClaimResult answers POST /api/workflow/work-items/{id}/claim.
type WorkItemClaimResult struct {
	WorkItem  repository.WorkItem `json:"workItem"`
	ClaimedBy string              `json:"claimedBy"`
	ClaimedAt time.Time           `json:"claimedAt"`
}

// ProcessInstanceRuntime answers GET /api/workflow/process-instances/{key}/runtime.
type ProcessInstanceRuntime struct {
	ProcessInstanceKey string                            `json:"processInstanceKey"`
	ZeebeStatus        string                            `json:"zeebeStatus"`
	ActiveElementID    string                            `json:"activeElementId"`
	Case               *repository.BusinessCase          `json:"case"`
	ActiveWorkTask     *repository.WorkItem              `json:"activeWorkTask"`
	PendingJobs        []service.ProcessJobSnapshot      `json:"pendingJobs"`
	Incidents          []service.ProcessIncidentSnapshot `json:"incidents"`
	PendingJobsError   string                            `json:"pendingJobsError"`
	Timeline           []repository.TimelineEvent        `json:"timeline"`
	Hint               string                            `json:"hint"`
	WorkerNote         string                            `json:"workerNote"`
}

// CaseVariables answers GET /api/workflow/cases/{id}/variables.
type CaseVariables struct {
	CaseID             string         `json:"case_id"`
	ProcessInstanceKey string         `json:"process_instance_key"`
	Variables          map[string]any `json:"variables"`
}

// TaskReadiness answers GET /api/workflow/cases/{id}/task-readiness.
type TaskReadiness struct {
	Ready  bool   `json:"ready"`
	Status string `json:"status"`
}

// CaseActorRequest is the body of the case submit and claim commands.
type CaseActorRequest struct {
	Actor          string         `json:"actor"`
	Variables      map[string]any `json:"variables"`
	IdempotencyKey string         `json:"idempotencyKey"`
}

// RetryRequest is the body of the job retry commands.
type RetryRequest struct {
	Retries int32 `json:"retries"`
}

// JobRetryResult answers the job retry commands.
type JobRetryResult struct {
	Status  string `json:"status"`
	JobKey  string `json:"jobKey"`
	Retries int32  `json:"retries"`
}

// ServiceJobsRetryResult answers POST /api/workflow/process-instances/{key}/retry-service-jobs.
type ServiceJobsRetryResult struct {
	Status  string   `json:"status"`
	Message string   `json:"message,omitempty"`
	Retried []string `json:"retried"`
}

// IncidentRetryResult answers POST /api/workflow/operate/incidents/{key}/retry.
type IncidentRetryResult struct {
	Status      string `json:"status"`
	IncidentKey string `json:"incidentKey,omitempty"`
	JobKey      string `json:"jobKey"`
}

// IncidentResolveResult answers the incident resolve commands.
type IncidentResolveResult struct {
	Status      string `json:"status"`
	IncidentKey string `json:"incidentKey"`
}

// OperateHistoryPage answers GET /api/workflow/operate/process-instances/{key}/history.
type OperateHistoryPage struct {
	Items      []service.ZeebeHistoryEvent `json:"items"`
	NextCursor string                      `json:"nextCursor"`
}

// UserTaskAssignRequest is the body of POST /api/workflow/operate/user-tasks/{key}/assign.
type UserTaskAssignRequest struct {
	Assignee string `json:"assignee"`
}

// UserTaskAssignResult answers POST /api/workflow/operate/user-tasks/{key}/assign.
type UserTaskAssignResult struct {
	Status      string `json:"status"`
	UserTaskKey string `json:"userTaskKey"`
	Assignee    string `json:"assignee"`
}

// SetVariablesRequest is the body of POST /api/workflow/operate/process-instances/{key}/variables.
type SetVariablesRequest struct {
	Variables          map[string]any `json:"variables"`
	ElementInstanceKey int64          `json:"elementInstanceKey"`
	Local              bool           `json:"local"`
}

// SetVariablesResult answers POST /api/workflow/operate/process-instances/{key}/variables.
type SetVariablesResult struct {
	Status             string `json:"status"`
	ElementInstanceKey string `json:"elementInstanceKey"`
	Variables          int    `json:"variables"`
}
