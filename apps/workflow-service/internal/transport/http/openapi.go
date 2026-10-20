package http

import (
	nethttp "net/http"
	"sort"

	"github.com/arda-labs/arda/apps/workflow-service/internal/handler"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	ardaopenapi "github.com/arda-labs/arda/libs/go/arda-openapi"
)

// OpenAPIDocument declares the public workflow REST surface. Each operation
// names the Go type its handler decodes and the type it writes as the envelope
// result, so contracts/openapi/workflow-v1.json is derived from the real wire
// shape. TestOpenAPIDocumentIsCommitted regenerates the document and fails
// when the committed copy differs; check-openapi-routes.mjs fails when a public
// route in router.go is missing here (ADR-005).
//
// Most workflow routes are registered as Go mux subtrees ("/api/workflow/x/")
// whose handler dispatches on the remaining path and r.Method; the operations
// below spell out every sub-path those handlers accept. BPMN uploads are
// multipart/form-data and the XML / XLSX downloads are raw bodies, declared
// with Form and Raw. The /internal/ai/* routes are service-to-service and are
// not part of this document.
func OpenAPIDocument() ardaopenapi.Document {
	type (
		op    = ardaopenapi.Operation
		param = ardaopenapi.Param
	)
	created, noContent := nethttp.StatusCreated, nethttp.StatusNoContent
	catalog := []param{{Name: "q", Description: "Free-text search"}, {Name: "sort"}, {Name: "order", Enum: []string{"asc", "desc"}}}
	period := []param{{Name: "from", Description: "Start, RFC3339 or YYYY-MM-DD"}, {Name: "to", Description: "End, RFC3339 or YYYY-MM-DD"}}
	workItems := []param{
		{Name: "direction", Enum: []string{"INCOMING", "OUTGOING", "ALL"}},
		{Name: "from", Description: "Created from, RFC3339 or YYYY-MM-DD"}, {Name: "fromDate", Description: "Alias of from"},
		{Name: "to", Description: "Created to, RFC3339 or YYYY-MM-DD"}, {Name: "toDate", Description: "Alias of to"},
		{Name: "accounting"}, {Name: "slaStatus"}, {Name: "sla_status", Description: "Alias of slaStatus"},
		{Name: "transactionStatus"}, {Name: "status", Description: "Alias of transactionStatus"},
		{Name: "node", Description: "Step code"}, {Name: "step_code", Description: "Alias of node"}, {Name: "currentStep", Description: "Alias of node"},
		{Name: "domain", Description: "Business area"}, {Name: "business_area", Description: "Alias of domain"},
		{Name: "limit", Type: "integer"},
	}
	cases := []param{{Name: "case_type"}, {Name: "status"}, {Name: "assigned_to"}, {Name: "candidate_role"}, {Name: "keyword"}, {Name: "limit", Type: "integer"}}
	page := []param{{Name: "pageSize", Type: "integer"}, {Name: "cursor", Type: "integer", Description: "Opaque cursor from nextCursor"}}
	operateInstances := append([]param{
		{Name: "state"}, {Name: "bpmnProcessId"}, {Name: "processDefinitionKey", Type: "integer"}, {Name: "processInstanceKey", Type: "integer"},
		{Name: "parentProcessInstanceKey", Type: "integer"}, {Name: "startFrom", Description: "RFC3339"}, {Name: "startTo", Description: "RFC3339"},
	}, page...)
	operateIncidents := append([]param{
		{Name: "state"}, {Name: "errorType"}, {Name: "bpmnProcessId"}, {Name: "processInstanceKey", Type: "integer"},
		{Name: "from", Description: "RFC3339"}, {Name: "to", Description: "RFC3339"},
	}, page...)
	operateJobs := append([]param{{Name: "state"}, {Name: "type"}, {Name: "bpmnProcessId"}, {Name: "processInstanceKey", Type: "integer"}, {Name: "elementId"}}, page...)
	operateUserTasks := append([]param{{Name: "state"}, {Name: "assignee"}, {Name: "candidateGroup"}, {Name: "bpmnProcessId"}, {Name: "processInstanceKey", Type: "integer"}, {Name: "elementId"}}, page...)
	bpmnForm := []ardaopenapi.FormField{
		{Name: "file", File: true, Required: true, Description: "BPMN XML"},
		{Name: "name"}, {Name: "resourceName", Description: "Defaults to the uploaded file name"}, {Name: "processCode"}, {Name: "status"},
	}
	xml := &ardaopenapi.RawResponse{MediaType: "application/xml"}
	xlsx := &ardaopenapi.RawResponse{MediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Binary: true}
	notImplemented := []int{401, 403, 501}
	mutation := []int{400, 401, 403, 500}
	upstream := []int{400, 401, 403, 404, 500, 502, 503}

	return ardaopenapi.Document{
		Title:       "Arda Workflow",
		Version:     "1.0.0",
		Description: "Business cases, work items, process definitions, workflow configuration and the Zeebe operate surface. Generated from apps/workflow-service/internal/transport/http/openapi.go; do not edit by hand.",
		TypeSchemas: map[string]map[string]any{
			"github.com/arda-labs/arda/apps/workflow-service/internal/repository.WorkItem":        {"$ref": "#/components/schemas/WorkItem"},
			"github.com/arda-labs/arda/apps/workflow-service/internal/service.WorkflowTask":       {"$ref": "#/components/schemas/WorkflowTask"},
			"github.com/arda-labs/arda/apps/workflow-service/internal/service.ProcessJobSnapshot": {"$ref": "#/components/schemas/ProcessJobSnapshot"},
			"github.com/arda-labs/arda/apps/workflow-service/internal/handler.flexInt64":          {"type": []any{"integer", "string"}, "description": "int64 as a number or a decimal string"},
		},
		Schemas: map[string]map[string]any{
			"WorkItem":           workItemSchema(),
			"WorkflowTask":       workflowTaskSchema(),
			"ProcessJobSnapshot": processJobSnapshotSchema(),
		},
		Operations: []op{
			// Process engine commands
			{Method: "POST", Path: "/api/workflow/deploy", ID: "deployWorkflow", Summary: "Deploy a BPMN file to Zeebe", Form: bpmnForm[:1], Result: handler.DeployResult{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/start", ID: "startWorkflow", Summary: "Start a process instance", Request: handler.StartRequest{}, Result: handler.StartResult{}},
			{Method: "POST", Path: "/api/workflow/messages", ID: "publishWorkflowMessage", Summary: "Publish a BPMN message", Request: handler.MessageRequest{}, Result: handler.StatusResult{}},
			{Method: "POST", Path: "/api/workflow/instances/{key}/cancel", ID: "cancelWorkflowInstance", Summary: "Cancel a process instance owned by the tenant", Result: handler.CancelResult{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/instances/mapping/{businessKey}", ID: "getWorkflowInstanceMapping", Summary: "Resolve the process instance of a business key", Result: repository.ProcessMapping{}},

			// Case types and registry steps
			{Method: "GET", Path: "/api/workflow/case-types", ID: "listCaseTypes", Summary: "List case types", Query: catalog, Result: repository.CaseType{}, List: true},
			{Method: "POST", Path: "/api/workflow/case-types", ID: "createCaseType", Summary: "Create a case type", Request: repository.CaseTypeUpsert{}, Result: repository.CaseType{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/case-types/{caseType}", ID: "updateCaseType", Summary: "Update a case type", Request: repository.CaseTypeUpsert{}, Result: repository.CaseType{}},
			{Method: "PUT", Path: "/api/workflow/case-types/{caseType}/process-config", ID: "updateCaseTypeProcessConfig", Summary: "Update the process configuration of a case type", Request: repository.ProcessConfigUpdate{}, Result: repository.CaseType{}},
			{Method: "GET", Path: "/api/workflow/case-types/{caseType}/steps", ID: "listCaseTypeSteps", Summary: "Registry step metadata of a case type", Query: []param{{Name: "version", Type: "integer", Description: "Registry version; the current one when omitted"}}, Result: handler.CaseTypeSteps{}},

			// Configuration catalogs
			{Method: "GET", Path: "/api/workflow/sla-policies", ID: "listSLAPolicies", Summary: "List SLA policies", Query: catalog, Result: repository.SLAPolicy{}, List: true},
			{Method: "POST", Path: "/api/workflow/sla-policies", ID: "createSLAPolicy", Summary: "Create an SLA policy", Request: repository.SLAPolicy{}, Result: repository.SLAPolicy{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/sla-policies/{id}", ID: "updateSLAPolicy", Summary: "Update an SLA policy", Request: repository.SLAPolicy{}, Result: repository.SLAPolicy{}},
			{Method: "GET", Path: "/api/workflow/description-templates", ID: "listDescriptionTemplates", Summary: "List description templates", Query: catalog, Result: repository.DescriptionTemplate{}, List: true},
			{Method: "POST", Path: "/api/workflow/description-templates", ID: "createDescriptionTemplate", Summary: "Create a description template", Request: repository.DescriptionTemplate{}, Result: repository.DescriptionTemplate{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/description-templates/{id}", ID: "updateDescriptionTemplate", Summary: "Update a description template", Request: repository.DescriptionTemplate{}, Result: repository.DescriptionTemplate{}},
			{Method: "GET", Path: "/api/workflow/roles", ID: "listProcessRoles", Summary: "List process roles", Result: repository.ProcessRole{}, List: true},
			{Method: "POST", Path: "/api/workflow/roles", ID: "createProcessRole", Summary: "Create a process role", Request: repository.ProcessRole{}, Result: repository.ProcessRole{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/roles/{id}", ID: "updateProcessRole", Summary: "Update a process role", Request: repository.ProcessRole{}, Result: repository.ProcessRole{}},
			{Method: "GET", Path: "/api/workflow/role-catalog", ID: "listWorkflowRoleCatalog", Summary: "List the workflow role catalog", Result: repository.WorkflowRoleCatalog{}, List: true},
			{Method: "POST", Path: "/api/workflow/role-catalog", ID: "createWorkflowRoleCatalogEntry", Summary: "Create a workflow role", Request: repository.WorkflowRoleCatalog{}, Result: repository.WorkflowRoleCatalog{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/role-catalog/{roleCode}", ID: "updateWorkflowRoleCatalogEntry", Summary: "Update a workflow role", Request: repository.WorkflowRoleCatalog{}, Result: repository.WorkflowRoleCatalog{}},
			{Method: "GET", Path: "/api/workflow/role-memberships", ID: "listWorkflowRoleMemberships", Summary: "List role memberships of the target tenant", Result: repository.WorkflowRoleMembership{}, List: true},
			{Method: "POST", Path: "/api/workflow/role-memberships", ID: "createWorkflowRoleMembership", Summary: "Create a role membership", Request: repository.WorkflowRoleMembership{}, Result: repository.WorkflowRoleMembership{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/role-memberships/{id}", ID: "updateWorkflowRoleMembership", Summary: "Update a role membership", Request: repository.WorkflowRoleMembership{}, Result: repository.WorkflowRoleMembership{}},
			{Method: "GET", Path: "/api/workflow/assignment-rules", ID: "listWorkflowAssignmentRules", Summary: "List assignment rules", Result: repository.WorkflowAssignmentRule{}, List: true},
			{Method: "POST", Path: "/api/workflow/assignment-rules", ID: "createWorkflowAssignmentRule", Summary: "Create an assignment rule", Request: repository.WorkflowAssignmentRule{}, Result: repository.WorkflowAssignmentRule{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/assignment-rules/{id}", ID: "updateWorkflowAssignmentRule", Summary: "Update an assignment rule", Request: repository.WorkflowAssignmentRule{}, Result: repository.WorkflowAssignmentRule{}},
			{Method: "GET", Path: "/api/workflow/assignment-rules/resolve", ID: "resolveWorkflowAssignmentRules", Summary: "Dry-run the assignment rules for one step", Query: []param{{Name: "case_type", Required: true}, {Name: "step_code", Required: true}}, Result: service.AssignmentResult{}},
			{Method: "GET", Path: "/api/workflow/delegations", ID: "listWorkflowDelegations", Summary: "List delegations of the target tenant", Result: repository.WorkflowDelegation{}, List: true},
			{Method: "POST", Path: "/api/workflow/delegations", ID: "createWorkflowDelegation", Summary: "Create a delegation", Request: repository.WorkflowDelegation{}, Result: repository.WorkflowDelegation{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/delegations/{id}", ID: "updateWorkflowDelegation", Summary: "Update a delegation", Request: repository.WorkflowDelegation{}, Result: repository.WorkflowDelegation{}},

			// Process definitions (BPMN models)
			{Method: "GET", Path: "/api/workflow/process-definitions", ID: "listProcessDefinitions", Summary: "List stored process definitions", Result: repository.ProcessDefinition{}, List: true},
			{Method: "POST", Path: "/api/workflow/process-definitions", ID: "importProcessDefinition", Summary: "Import a BPMN model", Form: bpmnForm, Result: repository.ProcessDefinition{}, Status: created, Problems: mutation},
			{Method: "PUT", Path: "/api/workflow/process-definitions/{id}", ID: "updateProcessDefinition", Summary: "Replace the BPMN model of a definition", Form: bpmnForm, Result: repository.ProcessDefinition{}},
			{Method: "DELETE", Path: "/api/workflow/process-definitions/{id}", ID: "deleteProcessDefinition", Summary: "Delete a process definition", Status: noContent},
			{Method: "GET", Path: "/api/workflow/process-definitions/{id}/xml", ID: "downloadProcessDefinitionXML", Summary: "Download the BPMN XML", Raw: xml},
			{Method: "POST", Path: "/api/workflow/process-definitions/{id}/deploy", ID: "deployProcessDefinition", Summary: "Deploy the stored model to Zeebe", Result: repository.ProcessDefinition{}, Problems: upstream},

			// Work items (inbox)
			{Method: "GET", Path: "/api/workflow/work-items", ID: "listWorkItems", Summary: "List the caller's work items", Query: workItems, Result: repository.WorkItem{}, List: true},
			{Method: "GET", Path: "/api/workflow/work-items/summary", ID: "getWorkItemSummary", Summary: "Work item counts per node", Query: workItems, Result: handler.WorkItemSummaryResult{}},
			{Method: "GET", Path: "/api/workflow/work-items/export", ID: "exportWorkItems", Summary: "Export the filtered work items as XLSX", Query: workItems, Raw: xlsx},
			{Method: "GET", Path: "/api/workflow/work-items/{id}", ID: "getWorkItem", Summary: "Read a work item", Result: repository.WorkItem{}},
			{Method: "POST", Path: "/api/workflow/work-items/{id}/claim", ID: "claimWorkItem", Summary: "Claim a work item for the caller", Result: handler.WorkItemClaimResult{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/analytics/overview", ID: "getWorkflowAnalyticsOverview", Summary: "Workflow analytics overview", Query: period, Result: repository.AnalyticsOverview{}},

			// Tasks, jobs and process instances
			{Method: "POST", Path: "/api/workflow/tasks/claim", ID: "claimWorkflowTask", Summary: "Claim the active user task of a case", Request: handler.ClaimTaskRequest{}, Result: service.WorkflowTask{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/tasks/{jobKey}/complete", ID: "completeWorkflowTask", Summary: "Complete a user task", Request: handler.CompleteUserTaskRequest{}, Result: handler.StatusResult{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/jobs/{jobKey}/retry", ID: "retryWorkflowJob", Summary: "Retry a failed job", Request: handler.RetryRequest{}, Result: handler.JobRetryResult{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/process-instances/{key}/runtime", ID: "getProcessInstanceRuntime", Summary: "Runtime diagnosis of a process instance", Result: handler.ProcessInstanceRuntime{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/process-instances/{key}/retry-service-jobs", ID: "retryProcessServiceJobs", Summary: "Retry the failed service jobs of a process instance", Result: handler.ServiceJobsRetryResult{}, Problems: upstream},

			// Business cases
			{Method: "GET", Path: "/api/workflow/cases", ID: "listCases", Summary: "List business cases", Query: cases, Result: []repository.BusinessCase{}, ResultName: "BusinessCases"},
			{Method: "POST", Path: "/api/workflow/cases", ID: "createCase", Summary: "Create a business case", Request: repository.CaseCreate{}, Result: repository.BusinessCase{}, Status: created, Problems: []int{400, 401, 403, 409, 500}},
			{Method: "GET", Path: "/api/workflow/cases/{id}", ID: "getCase", Summary: "Read a business case", Result: repository.BusinessCase{}},
			{Method: "GET", Path: "/api/workflow/cases/{id}/timeline", ID: "getCaseTimeline", Summary: "Timeline events of a case", Result: []repository.TimelineEvent{}, ResultName: "TimelineEvents"},
			{Method: "GET", Path: "/api/workflow/cases/{id}/task-readiness", ID: "getCaseTaskReadiness", Summary: "Whether the step's work item is ready", Query: []param{{Name: "stepCode", Required: true}}, Result: handler.TaskReadiness{}},
			{Method: "GET", Path: "/api/workflow/cases/{id}/variables", ID: "getCaseVariables", Summary: "Live process variables of a case", Result: handler.CaseVariables{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/cases/{id}/submit", ID: "submitCase", Summary: "Submit a case into its workflow", Request: handler.CaseActorRequest{}, Result: repository.BusinessCase{}, Problems: []int{400, 401, 403, 404, 409, 422, 500}},
			{Method: "POST", Path: "/api/workflow/cases/{id}/claim", ID: "claimCase", Summary: "Assign a case to the caller", Request: handler.CaseActorRequest{}, Result: repository.BusinessCase{}},
			{Method: "GET", Path: "/api/workflow/cases/{id}/monitor", ID: "getCaseMonitor", Summary: "Runtime incidents and user tasks of a case", Result: handler.CaseMonitorView{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/cases/{id}/incidents/{incidentKey}/resolve", ID: "resolveCaseIncident", Summary: "Resolve an incident of a case", Result: handler.IncidentResolveResult{}, Problems: upstream},

			// Operate (Zeebe runtime)
			{Method: "GET", Path: "/api/workflow/operate/process-definitions", ID: "operateListProcessDefinitions", Summary: "Process definitions with case counts", Result: []handler.OperateProcessDef{}, ResultName: "OperateProcessDefs"},
			{Method: "GET", Path: "/api/workflow/operate/process-instances", ID: "operateSearchProcessInstances", Summary: "Search process instances", Query: operateInstances, Result: handler.OperateInstancePage{}},
			{Method: "GET", Path: "/api/workflow/operate/process-instances/{key}", ID: "operateGetProcessInstance", Summary: "Process instance detail", Result: handler.OperateInstanceDetail{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/operate/process-instances/{key}/pause", ID: "operatePauseProcessInstance", Summary: "Mark the case of an instance as suspended", Result: handler.StatusResult{}},
			{Method: "POST", Path: "/api/workflow/operate/process-instances/{key}/resume", ID: "operateResumeProcessInstance", Summary: "Resume a suspended case", Result: handler.StatusResult{}},
			{Method: "POST", Path: "/api/workflow/operate/process-instances/{key}/cancel", ID: "operateCancelProcessInstance", Summary: "Cancel a process instance", Request: handler.CancelInstanceRequest{}, Result: handler.StatusResult{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/operate/process-instances/{key}/element-instances", ID: "operateListElementInstances", Summary: "Element instances of a process instance", Result: []service.ZeebeElementInstance{}, ResultName: "ZeebeElementInstances", Problems: upstream},
			{Method: "GET", Path: "/api/workflow/operate/process-instances/{key}/variables", ID: "operateListVariables", Summary: "Variables of a process instance", Result: []service.ZeebeVariable{}, ResultName: "ZeebeVariables", Problems: upstream},
			{Method: "POST", Path: "/api/workflow/operate/process-instances/{key}/variables", ID: "operateSetVariables", Summary: "Set variables on a process instance scope", Request: handler.SetVariablesRequest{}, Result: handler.SetVariablesResult{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/operate/process-instances/{key}/jobs", ID: "operateListInstanceJobs", Summary: "Jobs of a process instance", Result: []service.ZeebeJob{}, ResultName: "ZeebeJobs", Problems: upstream},
			{Method: "GET", Path: "/api/workflow/operate/process-instances/{key}/history", ID: "operateListInstanceHistory", Summary: "History events of a process instance", Query: []param{{Name: "cursor", Type: "integer"}, {Name: "limit", Type: "integer"}}, Result: handler.OperateHistoryPage{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/operate/incidents", ID: "operateSearchIncidents", Summary: "Search incidents", Query: operateIncidents, Result: handler.OperateIncidentPage{}},
			{Method: "POST", Path: "/api/workflow/operate/incidents/{incidentKey}/retry", ID: "operateRetryIncident", Summary: "Retry the job behind an incident", Result: handler.IncidentRetryResult{}, Problems: upstream},
			{Method: "POST", Path: "/api/workflow/operate/incidents/{incidentKey}/resolve", ID: "operateResolveIncident", Summary: "Resolve an incident", Result: handler.IncidentResolveResult{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/operate/jobs", ID: "operateSearchJobs", Summary: "Search jobs", Query: operateJobs, Result: handler.OperateJobPage{}},
			{Method: "PUT", Path: "/api/workflow/operate/jobs/{jobKey}/retries", ID: "operateUpdateJobRetries", Summary: "Update the retries of a job", Request: handler.RetryRequest{}, Result: handler.JobRetryResult{}, Problems: upstream},
			{Method: "GET", Path: "/api/workflow/operate/job-definitions", ID: "operateListJobDefinitions", Summary: "Job definitions derived from the stored BPMN", Query: []param{{Name: "bpmnProcessId"}}, Result: []handler.OperateJobDefinition{}, ResultName: "OperateJobDefinitions"},
			{Method: "POST", Path: "/api/workflow/operate/job-definitions/{id}/suspend", ID: "operateSuspendJobDefinition", Summary: "Not implemented: suspend a job definition", Result: handler.StatusResult{}, Problems: notImplemented},
			{Method: "POST", Path: "/api/workflow/operate/job-definitions/{id}/activate", ID: "operateActivateJobDefinition", Summary: "Not implemented: activate a job definition", Result: handler.StatusResult{}, Problems: notImplemented},
			{Method: "GET", Path: "/api/workflow/operate/element-stats", ID: "operateListElementStats", Summary: "Case counts per BPMN element", Result: []handler.OperateElementStat{}, ResultName: "OperateElementStats"},
			{Method: "GET", Path: "/api/workflow/operate/summary", ID: "operateGetSummary", Summary: "Tenant runtime counters", Result: handler.OperateSummary{}},
			{Method: "GET", Path: "/api/workflow/operate/user-tasks", ID: "operateSearchUserTasks", Summary: "Search user tasks", Query: operateUserTasks, Result: handler.OperateUserTaskPage{}},
			{Method: "POST", Path: "/api/workflow/operate/user-tasks/{key}/assign", ID: "operateAssignUserTask", Summary: "Assign a user task", Request: handler.UserTaskAssignRequest{}, Result: handler.UserTaskAssignResult{}, Problems: upstream},
		},
	}
}

// The three types below have a custom MarshalJSON that renders int64 keys as
// decimal strings and drops some fields, so their schemas are spelled out to
// match that output rather than the struct tags.

func objectSchema(required []string, props map[string]any) map[string]any {
	sort.Strings(required)
	list := make([]any, len(required))
	for i, name := range required {
		list[i] = name
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": list, "properties": props}
}

func workItemSchema() map[string]any {
	str := map[string]any{"type": "string"}
	nullableTime := map[string]any{"type": []any{"string", "null"}, "format": "date-time"}
	nullableStrings := map[string]any{"anyOf": []any{map[string]any{"type": "array", "items": str}, map[string]any{"type": "null"}}}
	props := map[string]any{}
	for _, name := range []string{
		"id", "caseId", "caseCode", "caseType", "direction", "primaryObjectType", "primaryObjectId", "taskType", "stepCode",
		"title", "description", "summary", "status", "transactionStatus", "createdBy", "createdByName", "createdByAvatar",
		"candidateRole", "candidateGroupId", "candidateOrgUnitId", "assignedTo", "assignedToName", "assignedToAvatar",
		"previousAssignedTo", "previousAssignedToName", "previousAssignedToAvatar", "slaStatus", "claimBlockedReason",
		"stepKind", "formKey", "processInstanceKey", "jobKey",
	} {
		props[name] = str
	}
	for _, name := range []string{"assignedAt", "claimExpiresAt", "slaDueAt"} {
		props[name] = nullableTime
	}
	for _, name := range []string{"canClaim", "canOpen", "canReassign"} {
		props[name] = map[string]any{"type": "boolean"}
	}
	props["allowedActions"] = nullableStrings
	props["requiredCommentOn"] = nullableStrings
	props["registryVersion"] = map[string]any{"type": "integer"}
	props["createdAt"] = map[string]any{"type": "string", "format": "date-time"}
	props["updatedAt"] = map[string]any{"type": "string", "format": "date-time"}
	required := make([]string, 0, len(props))
	for name := range props {
		if name != "processInstanceKey" && name != "jobKey" {
			required = append(required, name)
		}
	}
	schema := objectSchema(required, props)
	schema["x-go-type"] = "github.com/arda-labs/arda/apps/workflow-service/internal/repository.WorkItem"
	return schema
}

func workflowTaskSchema() map[string]any {
	str := map[string]any{"type": "string"}
	props := map[string]any{"variables": map[string]any{"type": []any{"object", "null"}, "additionalProperties": true}, "slaDueAt": map[string]any{"type": "string", "format": "date-time"}}
	for _, name := range []string{"jobKey", "type", "elementId", "processInstanceKey", "caseId", "caseCode", "customerId", "customerName", "candidateRole", "formKey"} {
		props[name] = str
	}
	schema := objectSchema([]string{"jobKey", "type", "elementId", "processInstanceKey", "caseId", "caseCode", "customerId", "customerName", "candidateRole", "formKey", "variables"}, props)
	schema["x-go-type"] = "github.com/arda-labs/arda/apps/workflow-service/internal/service.WorkflowTask"
	return schema
}

func processJobSnapshotSchema() map[string]any {
	str := map[string]any{"type": "string"}
	props := map[string]any{"jobType": str, "elementId": str, "processInstanceKey": str, "jobKey": str, "retries": map[string]any{"type": "integer"}, "state": str, "caseId": str, "errorMessage": str}
	schema := objectSchema([]string{"jobType", "elementId", "processInstanceKey", "jobKey", "retries", "state"}, props)
	schema["x-go-type"] = "github.com/arda-labs/arda/apps/workflow-service/internal/service.ProcessJobSnapshot"
	return schema
}
