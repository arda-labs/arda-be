package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

// ─── Response types ──────────────────────────────────────────────────────────────

type OperateProcessDef struct {
	ID            string                  `json:"id"`
	ProcessCode   string                  `json:"processCode"`
	Name          string                  `json:"name"`
	BpmnProcessID string                  `json:"bpmnProcessId"`
	Version       int                     `json:"version"`
	ResourceName  string                  `json:"resourceName"`
	Status        string                  `json:"status"`
	DeploymentKey *int64                  `json:"deploymentKey,omitempty"`
	DeployedAt    *string                 `json:"deployedAt,omitempty"`
	InstanceCount int                     `json:"instanceCount"`
	IncidentCount int                     `json:"incidentCount"`
	ActiveCount   int                     `json:"activeCount"`
	ElementStats  []OperateElementStat    `json:"elementStats"`
}

type OperateProcessInstance struct {
	ProcessInstanceKey  string `json:"processInstanceKey"`
	BpmnProcessId       string `json:"bpmnProcessId"`
	Version             int    `json:"version"`
	BusinessKey         string `json:"businessKey,omitempty"`
	State               string `json:"state"`
	ElementId           string `json:"elementId,omitempty"`
	StartTime           string `json:"startTime"`
	RunningDuration     string `json:"runningDuration,omitempty"`
}

type OperateIncident struct {
	IncidentKey         string `json:"incidentKey"`
	ProcessInstanceKey  string `json:"processInstanceKey"`
	BpmnProcessId       string `json:"bpmnProcessId"`
	ElementId           string `json:"elementId"`
	ElementInstanceKey  string `json:"elementInstanceKey"`
	JobKey              string `json:"jobKey,omitempty"`
	ErrorType           string `json:"errorType"`
	ErrorMessage        string `json:"errorMessage"`
	State               string `json:"state"`
	CreatedAt           string `json:"createdAt"`
}

type OperateJob struct {
	JobKey              string `json:"jobKey"`
	Type                string `json:"type"`
	ProcessInstanceKey  string `json:"processInstanceKey"`
	BpmnProcessId       string `json:"bpmnProcessId"`
	ElementId           string `json:"elementId"`
	State               string `json:"state"`
	Retries             int    `json:"retries"`
	MaxRetries          int    `json:"maxRetries"`
	CreatedAt           string `json:"createdAt"`
	Worker              string `json:"worker,omitempty"`
	ErrorMessage        string `json:"errorMessage,omitempty"`
}

type OperateJobDefinition struct {
	JobDefinitionKey    string `json:"jobDefinitionKey"`
	Type                string `json:"type"`
	ProcessDefinitionKey string `json:"processDefinitionKey"`
	BpmnProcessId       string `json:"bpmnProcessId"`
	State               string `json:"state"`
	Retries             int    `json:"retries"`
	CreatedAt           string `json:"createdAt"`
}

type OperateElementStat struct {
	BpmnProcessId  string `json:"bpmnProcessId"`
	ElementId      string `json:"elementId"`
	ElementName    string `json:"elementName"`
	ElementType    string `json:"elementType"`
	ActiveCount    int    `json:"activeCount"`
	CompletedCount int    `json:"completedCount"`
	IncidentCount  int    `json:"incidentCount"`
	TotalCount     int    `json:"totalCount"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────────────

func operateDateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func caseState(bc *repository.BusinessCase) string {
	if bc == nil {
		return "UNKNOWN"
	}
	s := bc.Status
	if s == "COMPLETED" {
		return "COMPLETED"
	}
	if s == "CANCELLED" {
		return "CANCELED"
	}
	if s == "SUSPENDED" {
		return "SUSPENDED"
	}
	if s == "FAILED" || s == "INCIDENT" {
		return "INCIDENT"
	}
	return "ACTIVE"
}

func formatDuration(start time.Time, end *time.Time) string {
	var e time.Time
	if end != nil && !end.IsZero() {
		e = *end
	} else {
		e = time.Now()
	}
	d := e.Sub(start)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(m) + "m"
}

// incidentErrorType maps event types to operate-style error types.
func incidentErrorType(eventType string) string {
	switch eventType {
	case "JOB_FAILED":
		return "JOB_FAILED"
	case "INCIDENT":
		return "IO_MAPPING"
	default:
		return eventType
	}
}

// ─── Handlers ────────────────────────────────────────────────────────────────────

func (h *WorkflowHandler) OperateProcessDefinitions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}

	defs, err := h.processDefinition.List(r.Context())
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query process definitions: "+err.Error())
		return
	}

	// Count cases per bpmnProcessId
	cases, err := h.caseRepo.ListCases(r.Context(), repository.CaseListFilter{Limit: 500})
	if err != nil {
		cases = nil
	}
	caseCounts := map[string]struct{ total, incident, active int }{}
	for _, c := range cases {
		if c.BpmnProcessID == nil {
			continue
		}
		pid := *c.BpmnProcessID
		entry := caseCounts[pid]
		entry.total++
		if c.Status == "FAILED" || c.Status == "INCIDENT" {
			entry.incident++
		} else if c.Status != "COMPLETED" && c.Status != "CANCELLED" {
			entry.active++
		}
		caseCounts[pid] = entry
	}

	out := make([]OperateProcessDef, 0, len(defs))
	for _, d := range defs {
		counts := caseCounts[d.BpmnProcessID]
		elementStats := deriveElementStats(cases, d.BpmnProcessID)
		deployedAt := ""
		if d.DeployedAt != nil {
			deployedAt = d.DeployedAt.Format(time.RFC3339)
		}
		out = append(out, OperateProcessDef{
			ID:            d.ID,
			ProcessCode:   d.ProcessCode,
			Name:          d.Name,
			BpmnProcessID: d.BpmnProcessID,
			Version:       d.Version,
			ResourceName:  d.ResourceName,
			Status:        d.Status,
			DeploymentKey: d.DeploymentKey,
			DeployedAt:    &deployedAt,
			InstanceCount: counts.total,
			IncidentCount: counts.incident,
			ActiveCount:   counts.active,
			ElementStats:  elementStats,
		})
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (h *WorkflowHandler) OperateProcessInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}

	cases, err := h.caseRepo.ListCases(r.Context(), repository.CaseListFilter{Limit: 500})
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query cases: "+err.Error())
		return
	}

	out := make([]OperateProcessInstance, 0, len(cases))
	for _, c := range cases {
		if c.ProcessInstanceKey == nil {
			continue // Skip cases without Zeebe process instance
		}
		bpmnId := ""
		if c.BpmnProcessID != nil {
			bpmnId = *c.BpmnProcessID
		}
		version := 0
		if c.BpmnVersion != nil {
			version = *c.BpmnVersion
		}
		out = append(out, OperateProcessInstance{
			ProcessInstanceKey: strconv.FormatInt(*c.ProcessInstanceKey, 10),
			BpmnProcessId:      bpmnId,
			Version:            version,
			BusinessKey:        c.CaseCode,
			State:              caseState(&c),
			ElementId:          c.CurrentStep,
			StartTime:          operateDateTime(c.CreatedAt),
			RunningDuration:    formatDuration(c.CreatedAt, nil),
		})
	}
	if out == nil {
		out = []OperateProcessInstance{}
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (h *WorkflowHandler) OperateIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}

	cases, err := h.caseRepo.ListCases(r.Context(), repository.CaseListFilter{Limit: 500})
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query cases: "+err.Error())
		return
	}

	out := make([]OperateIncident, 0)
	for _, c := range cases {
		if c.ProcessInstanceKey == nil {
			continue
		}
		timeline, err := h.caseRepo.ListTimeline(r.Context(), c.ID)
		if err != nil || len(timeline) == 0 {
			continue
		}
		bpmnId := ""
		if c.BpmnProcessID != nil {
			bpmnId = *c.BpmnProcessID
		}
		for _, ev := range timeline {
			if ev.EventType != "JOB_FAILED" {
				continue
			}
			inc, ok := parseJobFailureNote(ev.Note)
			if !ok {
				continue
			}
			state := "CREATED"
			// If case moved past this state, consider it resolved
			if caseState(&c) != "INCIDENT" {
				state = "RESOLVED"
			}
			out = append(out, OperateIncident{
				IncidentKey:        "inc-" + strconv.FormatInt(ev.ID, 10),
				ProcessInstanceKey: strconv.FormatInt(*c.ProcessInstanceKey, 10),
				BpmnProcessId:      bpmnId,
				ElementId:          inc.ElementID,
				ElementInstanceKey: strconv.FormatInt(*c.ProcessInstanceKey, 10),
				JobKey:             inc.JobKey,
				ErrorType:          incidentErrorType(ev.EventType),
				ErrorMessage:       inc.ErrorMessage,
				State:              state,
				CreatedAt:          ev.CreatedAt.Format(time.RFC3339),
			})
		}
	}
	if out == nil {
		out = []OperateIncident{}
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (h *WorkflowHandler) OperateJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}

	if h.zeebeSvc == nil {
		writeJSON(w, r, http.StatusOK, []OperateJob{})
		return
	}

	cases, err := h.caseRepo.ListCases(r.Context(), repository.CaseListFilter{Limit: 100})
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query cases: "+err.Error())
		return
	}

	out := make([]OperateJob, 0)
	for _, c := range cases {
		if c.ProcessInstanceKey == nil || c.BpmnProcessID == nil {
			continue
		}
		jobs, err := h.zeebeSvc.FindProcessJobs(r.Context(), *c.ProcessInstanceKey)
		if err != nil || len(jobs) == 0 {
			continue
		}
		for _, j := range jobs {
			state := "ACTIVATABLE"
			if j.Retries <= 0 {
				state = "FAILED"
			}
			out = append(out, OperateJob{
				JobKey:             strconv.FormatInt(j.JobKey, 10),
				Type:               j.JobType,
				ProcessInstanceKey: strconv.FormatInt(j.ProcessInstanceKey, 10),
				BpmnProcessId:      *c.BpmnProcessID,
				ElementId:          j.ElementID,
				State:              state,
				Retries:            int(j.Retries),
				MaxRetries:         3,
				CreatedAt:          operateDateTime(time.Now()),
				ErrorMessage:       j.ErrorMessage,
			})
		}
	}
	if out == nil {
		out = []OperateJob{}
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (h *WorkflowHandler) OperateJobDefinitions(w http.ResponseWriter, r *http.Request) {
	// Job definitions require Zeebe REST API which may not be available.
	// Return empty array for now.
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	writeJSON(w, r, http.StatusOK, []OperateJobDefinition{})
}

func (h *WorkflowHandler) OperateElementStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}

	cases, err := h.caseRepo.ListCases(r.Context(), repository.CaseListFilter{Limit: 500})
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query cases: "+err.Error())
		return
	}

	out := deriveElementStats(cases, "")
	if out == nil {
		out = []OperateElementStat{}
	}
	writeJSON(w, r, http.StatusOK, out)
}

// deriveElementStats builds element stats from cases grouped by currentStep.
func deriveElementStats(cases []repository.BusinessCase, filterBpmnId string) []OperateElementStat {
	type stepKey struct{ bpmnId, step string }
	groups := map[stepKey]struct{ total, active, incident, completed int }{}
	for _, c := range cases {
		if c.BpmnProcessID == nil {
			continue
		}
		pid := *c.BpmnProcessID
		if filterBpmnId != "" && pid != filterBpmnId {
			continue
		}
		step := c.CurrentStep
		if step == "" {
			step = "StartEvent_Submit"
		}
		key := stepKey{pid, step}
		entry := groups[key]
		entry.total++
		switch c.Status {
		case "COMPLETED":
			entry.completed++
		case "FAILED", "INCIDENT":
			entry.incident++
		default:
			entry.active++
		}
		groups[key] = entry
	}

	out := make([]OperateElementStat, 0, len(groups))
	for key, g := range groups {
		out = append(out, OperateElementStat{
			BpmnProcessId:  key.bpmnId,
			ElementId:      key.step,
			ElementName:    key.step,
			ElementType:    "userTask",
			ActiveCount:    g.active,
			CompletedCount: g.completed,
			IncidentCount:  g.incident,
			TotalCount:     g.total,
		})
	}
	return out
}

// ─── Instance actions ────────────────────────────────────────────────────────────

func (h *WorkflowHandler) OperatePauseInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	key, ok := parsePathInt64(r.URL.Path, "/api/workflow/operate/process-instances/", "/pause")
	if !ok {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid process instance key")
		return
	}
	// Zeebe does not have a native pause; we mark the case as SUSPENDED
	_ = h.caseRepo.SetCaseStatusByProcessKey(r.Context(), key, "SUSPENDED")
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "paused"})
}

func (h *WorkflowHandler) OperateResumeInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	key, ok := parsePathInt64(r.URL.Path, "/api/workflow/operate/process-instances/", "/resume")
	if !ok {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid process instance key")
		return
	}
	_ = h.caseRepo.SetCaseStatusByProcessKey(r.Context(), key, "ACTIVE")
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "resumed"})
}

func (h *WorkflowHandler) OperateCancelInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	key, ok := parsePathInt64(r.URL.Path, "/api/workflow/operate/process-instances/", "/cancel")
	if !ok {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid process instance key")
		return
	}
	_ = h.caseRepo.SetCaseStatusByProcessKey(r.Context(), key, "CANCELLED")
	if h.zeebeSvc != nil {
		_ = h.zeebeSvc.CancelWorkflow(r.Context(), key)
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *WorkflowHandler) OperateRetryIncident(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	// Parse jobKey from path: /api/workflow/operate/incidents/{incidentKey}/retry
	// We store incidentKey = inc-{timelineId} format, or jobKey directly
	rest := strings.TrimPrefix(r.URL.Path, "/api/workflow/operate/incidents/")
	parts := strings.Split(strings.TrimSuffix(rest, "/retry"), "/retry")
	if len(parts) == 0 || parts[0] == "" {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid incident key")
		return
	}
	keyStr := strings.TrimPrefix(parts[0], "inc-")
	jobKey, err := strconv.ParseInt(keyStr, 10, 64)
	if err != nil || jobKey <= 0 {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid job key: "+parts[0])
		return
	}
	if h.zeebeSvc == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe service is not configured")
		return
	}
	if err := h.zeebeSvc.RetryJob(r.Context(), jobKey, 3); err != nil {
		writeAPIError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "retried", "jobKey": keyStr})
}

func (h *WorkflowHandler) OperateResolveIncident(w http.ResponseWriter, r *http.Request) {
	// Marking resolved — in Zeebe, incidents auto-resolve when the job succeeds.
	// We just acknowledge the resolution.
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "resolved"})
}

func (h *WorkflowHandler) OperateUpdateJobRetries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeMethodNotAllowed(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/workflow/operate/jobs/")
	parts := strings.Split(strings.TrimSuffix(rest, "/retries"), "/retries")
	if len(parts) == 0 || parts[0] == "" {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid job key")
		return
	}
	jobKey, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || jobKey <= 0 {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid job key")
		return
	}
	var req struct {
		Retries int32 `json:"retries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Retries <= 0 {
		req.Retries = 3
	}
	if h.zeebeSvc == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe service is not configured")
		return
	}
	if err := h.zeebeSvc.RetryJob(r.Context(), jobKey, req.Retries); err != nil {
		writeAPIError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"status":  "updated",
		"jobKey":  parts[0],
		"retries": req.Retries,
	})
}

func (h *WorkflowHandler) OperateSuspendJobDef(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	// Job definition suspend/activate requires Zeebe REST API (not in gRPC).
	writeAPIError(w, r, http.StatusNotImplemented, "Job definition suspend/activate requires Zeebe REST API")
}

func (h *WorkflowHandler) OperateActivateJobDef(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	writeAPIError(w, r, http.StatusNotImplemented, "Job definition suspend/activate requires Zeebe REST API")
}

// ─── Path parsing helpers ────────────────────────────────────────────────────────

// parsePathInt64 extracts an int64 from a URL path by removing prefix and suffix.
func parsePathInt64(path, prefix, suffix string) (int64, bool) {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}
