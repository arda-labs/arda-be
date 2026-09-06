package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// CaseMonitorView is the case-scoped monitoring payload fed by real Zeebe
// exporter data: open incidents and live user tasks for the underlying
// process instance. Closes workflow-service Known Gap #3 (monitor panel
// previously only saw locally-synthesized incident records).
type CaseMonitorView struct {
	CaseID             string                 `json:"caseId"`
	CaseCode           string                 `json:"caseCode"`
	CaseType           string                 `json:"caseType"`
	Status             string                 `json:"status"`
	CurrentStep        string                 `json:"currentStep"`
	ProcessInstanceKey string                 `json:"processInstanceKey,omitempty"`
	BpmnProcessID      string                 `json:"bpmnProcessId,omitempty"`
	SLADueAt           *string                `json:"slaDueAt,omitempty"`
	OpenIncidents      []service.ZeebeIncident `json:"openIncidents"`
	ActiveUserTasks    []service.ZeebeUserTask `json:"activeUserTasks"`
	IncidentSource     string                 `json:"incidentSource"`
}

func (h *WorkflowHandler) CaseMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	caseID := r.PathValue("id")
	bc, err := h.caseRepo.GetCase(r.Context(), caseID)
	if err != nil || bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "Case not found: "+caseID)
		return
	}

	view := CaseMonitorView{
		CaseID:          bc.ID,
		CaseCode:        bc.CaseCode,
		CaseType:        bc.CaseType,
		Status:          bc.Status,
		CurrentStep:     bc.CurrentStep,
		BpmnProcessID:   derefOrEmpty(bc.BpmnProcessID),
		OpenIncidents:   []service.ZeebeIncident{},
		ActiveUserTasks: []service.ZeebeUserTask{},
		IncidentSource:  "unavailable",
	}
	if bc.ProcessInstanceKey != nil && *bc.ProcessInstanceKey > 0 {
		view.ProcessInstanceKey = strconv.FormatInt(*bc.ProcessInstanceKey, 10)
	}
	if bc.SLADueAt != nil {
		due := bc.SLADueAt.Format("2006-01-02T15:04:05Z07:00")
		view.SLADueAt = &due
	}

	if h.IncidentIndex != nil && h.IncidentIndex.Enabled() && bc.ProcessInstanceKey != nil && *bc.ProcessInstanceKey > 0 {
		incidents, err := h.IncidentIndex.SearchIncidents(r.Context(), *bc.ProcessInstanceKey)
		if err != nil {
			// Monitor is read-only diagnostics: degraded data beats a 5xx.
			view.IncidentSource = "degraded"
		} else {
			view.OpenIncidents = incidents
			view.IncidentSource = "zeebe-exporter"
		}
	}
	if h.zeebeRest != nil && h.zeebeRest.Enabled() && bc.ProcessInstanceKey != nil && *bc.ProcessInstanceKey > 0 {
		if tasks, err := h.zeebeRest.SearchUserTasks(r.Context(), *bc.ProcessInstanceKey, "CREATED"); err == nil {
			view.ActiveUserTasks = tasks
		}
	}

	writeJSON(w, r, http.StatusOK, view)
}

// ResolveCaseIncident acknowledges an incident through the Zeebe gateway
// (POST /v2/incidents/{key}/resolution) after operators fixed the cause.
func (h *WorkflowHandler) ResolveCaseIncident(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	if h.zeebeRest == nil || !h.zeebeRest.Enabled() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe REST client is not configured")
		return
	}
	incidentKey, err := strconv.ParseInt(r.PathValue("incidentKey"), 10, 64)
	if err != nil || incidentKey <= 0 {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid incident key")
		return
	}
	if err := h.zeebeRest.ResolveIncident(r.Context(), incidentKey); err != nil {
		writeAPIError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "resolved", "incidentKey": strconv.FormatInt(incidentKey, 10)})
}

// ResolveAssignmentRules is an admin dry-run: shows which role and candidate
// users the persisted assignment rules would produce for a case-type/step.
func (h *WorkflowHandler) ResolveAssignmentRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if h.AssignmentResolver == nil || !h.AssignmentResolver.Enabled() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Assignment resolver is not configured")
		return
	}
	caseType := strings.TrimSpace(r.URL.Query().Get("case_type"))
	stepCode := strings.TrimSpace(r.URL.Query().Get("step_code"))
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	createdBy := strings.TrimSpace(r.URL.Query().Get("created_by"))
	if caseType == "" || stepCode == "" {
		writeAPIError(w, r, http.StatusBadRequest, "case_type and step_code are required")
		return
	}
	result := h.AssignmentResolver.Resolve(r.Context(), service.AssignmentRequest{
		CaseType:  caseType,
		StepCode:  stepCode,
		TenantID:  tenantID,
		CreatedBy: createdBy,
	})
	writeJSON(w, r, http.StatusOK, result)
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
