package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/handler"
)

func NewRouter(wfHandler *handler.WorkflowHandler) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/health/ready", wfHandler.HealthReady)

	// Workflow APIs
	mux.HandleFunc("/api/v1/workflows/deploy", wfHandler.Deploy)
	mux.HandleFunc("/api/workflow/deploy", wfHandler.Deploy)
	mux.HandleFunc("/api/v1/workflows/start", wfHandler.Start)
	mux.HandleFunc("/api/workflow/start", wfHandler.Start)
	mux.HandleFunc("/api/v1/workflows/messages", wfHandler.PublishMessage)
	mux.HandleFunc("/api/workflow/messages", wfHandler.PublishMessage)
	mux.HandleFunc("/api/workflow/case-types", wfHandler.CaseTypes)
	mux.HandleFunc("/api/workflow/case-types/", wfHandler.CaseTypeByID)
	mux.HandleFunc("/api/workflow/sla-policies", wfHandler.SLAPolicies)
	mux.HandleFunc("/api/workflow/sla-policies/", wfHandler.SLAPolicyByID)
	mux.HandleFunc("/api/workflow/description-templates", wfHandler.DescriptionTemplates)
	mux.HandleFunc("/api/workflow/description-templates/", wfHandler.DescriptionTemplateByID)
	mux.HandleFunc("/api/workflow/process-definitions", wfHandler.ProcessDefinitions)
	mux.HandleFunc("/api/workflow/process-definitions/", wfHandler.ProcessDefinitionByID)
	mux.HandleFunc("/api/workflow/roles", wfHandler.ProcessRoles)
	mux.HandleFunc("/api/workflow/roles/", wfHandler.ProcessRoleByID)
	mux.HandleFunc("/api/workflow/role-catalog", wfHandler.RoleCatalog)
	mux.HandleFunc("/api/workflow/role-catalog/", wfHandler.RoleCatalogByCode)
	mux.HandleFunc("/api/workflow/role-memberships", wfHandler.RoleMemberships)
	mux.HandleFunc("/api/workflow/role-memberships/", wfHandler.RoleMembershipByID)
	mux.HandleFunc("/api/workflow/assignment-rules", wfHandler.AssignmentRules)
	mux.HandleFunc("/api/workflow/assignment-rules/", wfHandler.AssignmentRuleByID)
	mux.HandleFunc("/api/workflow/delegations", wfHandler.Delegations)
	mux.HandleFunc("/api/workflow/delegations/", wfHandler.DelegationByID)
	mux.HandleFunc("/api/workflow/work-items", wfHandler.WorkItems)
	mux.HandleFunc("/api/workflow/work-items/summary", wfHandler.WorkItemSummary)
	mux.HandleFunc("/api/workflow/work-items/", wfHandler.WorkItemByID)
	mux.HandleFunc("/api/workflow/process-instances/", wfHandler.ProcessInstanceByKey)
	mux.HandleFunc("/api/workflow/jobs/", wfHandler.JobByKey)
	mux.HandleFunc("/api/workflow/tasks", wfHandler.Tasks)
	mux.HandleFunc("/api/workflow/tasks/claim", wfHandler.ClaimTask)
	mux.HandleFunc("/api/workflow/tasks/", wfHandler.TaskByID)
	mux.HandleFunc("/api/workflow/cases", wfHandler.Cases)
	mux.HandleFunc("/api/workflow/cases/", wfHandler.CaseByID)

	// Operate APIs
	mux.HandleFunc("/api/workflow/operate/process-definitions", wfHandler.OperateProcessDefinitions)
	mux.HandleFunc("/api/workflow/operate/process-instances", wfHandler.OperateProcessInstances)
	mux.HandleFunc("/api/workflow/operate/process-instances/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, "/pause") {
			wfHandler.OperatePauseInstance(w, r)
		} else if strings.HasSuffix(p, "/resume") {
			wfHandler.OperateResumeInstance(w, r)
		} else if strings.HasSuffix(p, "/cancel") {
			wfHandler.OperateCancelInstance(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/workflow/operate/incidents", wfHandler.OperateIncidents)
	mux.HandleFunc("/api/workflow/operate/incidents/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, "/retry") {
			wfHandler.OperateRetryIncident(w, r)
		} else if strings.HasSuffix(p, "/resolve") {
			wfHandler.OperateResolveIncident(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/workflow/operate/jobs", wfHandler.OperateJobs)
	mux.HandleFunc("/api/workflow/operate/jobs/", wfHandler.OperateUpdateJobRetries)
	mux.HandleFunc("/api/workflow/operate/job-definitions", wfHandler.OperateJobDefinitions)
	mux.HandleFunc("/api/workflow/operate/job-definitions/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, "/suspend") {
			wfHandler.OperateSuspendJobDef(w, r)
		} else if strings.HasSuffix(p, "/activate") {
			wfHandler.OperateActivateJobDef(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/workflow/operate/element-stats", wfHandler.OperateElementStats)

	// Dynamic paths
	mux.HandleFunc("/api/v1/workflows/instances/", func(w http.ResponseWriter, r *http.Request) {
		// e.g. /api/v1/workflows/instances/{instanceKey}/cancel
		if r.Method == http.MethodPost && len(r.URL.Path) > len("/api/v1/workflows/instances/") {
			if r.URL.Path[len(r.URL.Path)-len("/cancel"):] == "/cancel" {
				wfHandler.Cancel(w, r)
				return
			}
		}

		// e.g. /api/v1/workflows/instances/mapping/{businessKey}
		if r.Method == http.MethodGet && len(r.URL.Path) > len("/api/v1/workflows/instances/mapping/") {
			wfHandler.GetMapping(w, r)
			return
		}

		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/workflow/instances/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && len(r.URL.Path) > len("/api/workflow/instances/") {
			if r.URL.Path[len(r.URL.Path)-len("/cancel"):] == "/cancel" {
				wfHandler.Cancel(w, r)
				return
			}
		}

		if r.Method == http.MethodGet && len(r.URL.Path) > len("/api/workflow/instances/mapping/") {
			wfHandler.GetMapping(w, r)
			return
		}

		http.NotFound(w, r)
	})

	return mux
}
