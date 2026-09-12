package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// Runtime monitoring reads (Operate replacement). Everything is scoped to the
// caller tenant through business_cases: Zeebe records on this deployment all
// carry the <default> tenant id, so the case registry is the tenant boundary.

type operateInstanceRow struct {
	service.ZeebeProcessInstance
	CaseID        string `json:"caseId,omitempty"`
	BusinessKey   string `json:"businessKey,omitempty"`
	CaseType      string `json:"caseType,omitempty"`
	CaseStatus    string `json:"caseStatus,omitempty"`
	OpenIncidents int    `json:"openIncidents"`
}

type operateInstancePage struct {
	Items      []operateInstanceRow `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
	Source     string               `json:"source"`
}

type operateInstanceDetail struct {
	service.ZeebeProcessInstance
	CaseID        string  `json:"caseId,omitempty"`
	BusinessKey   string  `json:"businessKey,omitempty"`
	CaseType      string  `json:"caseType,omitempty"`
	CaseStatus    string  `json:"caseStatus,omitempty"`
	Title         string  `json:"title,omitempty"`
	SLADueAt      *string `json:"slaDueAt,omitempty"`
	OpenIncidents int     `json:"openIncidents"`
}

type operateIncidentRow struct {
	IncidentKey        string `json:"incidentKey"`
	ProcessInstanceKey string `json:"processInstanceKey"`
	BpmnProcessID      string `json:"bpmnProcessId,omitempty"`
	ElementID          string `json:"elementId,omitempty"`
	ElementInstanceKey string `json:"elementInstanceKey,omitempty"`
	JobKey             string `json:"jobKey,omitempty"`
	ErrorType          string `json:"errorType,omitempty"`
	ErrorMessage       string `json:"errorMessage,omitempty"`
	State              string `json:"state"`
	CreatedAt          string `json:"createdAt,omitempty"`
	CaseID             string `json:"caseId,omitempty"`
	BusinessKey        string `json:"businessKey,omitempty"`
}

type operateIncidentPage struct {
	Items      []operateIncidentRow `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
	Source     string               `json:"source"`
}

func (h *WorkflowHandler) operateSearchProcessInstances(w http.ResponseWriter, r *http.Request) {
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		items := h.operateInstancesFromDB(r.Context())
		rows := make([]operateInstanceRow, 0, len(items))
		for _, item := range items {
			rows = append(rows, operateInstanceRow{
				ZeebeProcessInstance: service.ZeebeProcessInstance{
					ProcessInstanceKey: item.ProcessInstanceKey,
					BpmnProcessID:      item.BpmnProcessId,
					Version:            item.Version,
					State:              item.State,
					StartTime:          item.StartTime,
				},
				BusinessKey: item.BusinessKey,
			})
		}
		writeJSON(w, r, http.StatusOK, operateInstancePage{Items: rows, Source: "database"})
		return
	}

	pageSize := clampOperatePageSize(int(queryInt64(r, "pageSize")))
	params := service.ProcessInstanceSearchParams{
		State:                    strings.TrimSpace(r.URL.Query().Get("state")),
		BpmnProcessID:            strings.TrimSpace(r.URL.Query().Get("bpmnProcessId")),
		ProcessDefinitionKey:     queryInt64(r, "processDefinitionKey"),
		ProcessInstanceKey:       queryInt64(r, "processInstanceKey"),
		ParentProcessInstanceKey: queryInt64(r, "parentProcessInstanceKey"),
		StartFrom:                queryTime(r, "startFrom"),
		StartTo:                  queryTime(r, "startTo"),
		PageSize:                 pageSize,
		Cursor:                   queryInt64(r, "cursor"),
	}

	rows := make([]operateInstanceRow, 0, pageSize)
	cursor := ""
	for attempt := 0; attempt < 4; attempt++ {
		items, next, err := h.MonitoringIndex.SearchProcessInstances(r.Context(), params)
		if err != nil {
			writeAPIError(w, r, http.StatusBadGateway, "Runtime monitoring is unavailable: "+err.Error())
			return
		}
		keys := make([]int64, 0, len(items))
		for _, item := range items {
			if key, err := strconv.ParseInt(item.ProcessInstanceKey, 10, 64); err == nil {
				keys = append(keys, key)
			}
		}
		cases, err := h.caseRepo.CasesByProcessInstanceKeys(r.Context(), keys)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope instances: "+err.Error())
			return
		}
		incidentCounts := map[int64]int{}
		if counts, err := h.MonitoringIndex.OpenIncidentCounts(r.Context(), keys); err == nil {
			incidentCounts = counts
		}
		for _, item := range items {
			key, _ := strconv.ParseInt(item.ProcessInstanceKey, 10, 64)
			bc, owned := cases[key]
			if !owned {
				continue
			}
			rows = append(rows, operateInstanceRow{
				ZeebeProcessInstance: item,
				CaseID:               bc.ID,
				BusinessKey:          bc.CaseCode,
				CaseType:             bc.CaseType,
				CaseStatus:           bc.Status,
				OpenIncidents:        incidentCounts[key],
			})
		}
		cursor = next
		if len(rows) >= pageSize || cursor == "" {
			break
		}
		params.Cursor = parseCursor(cursor)
		if params.Cursor <= 0 {
			cursor = ""
			break
		}
	}

	writeJSON(w, r, http.StatusOK, operateInstancePage{Items: rows, NextCursor: cursor, Source: "zeebe-exporter"})
}

func (h *WorkflowHandler) OperateProcessInstanceDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	key, ok := parsePathInt64(r.URL.Path, "/api/workflow/operate/process-instances/", "")
	if !ok {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid process instance key")
		return
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), key)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope instance: "+err.Error())
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "Process instance not found")
		return
	}
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Runtime monitoring requires ZEEBE_ES_URL")
		return
	}
	instance, err := h.MonitoringIndex.GetProcessInstance(r.Context(), key)
	if err != nil {
		writeAPIError(w, r, http.StatusBadGateway, "Runtime monitoring is unavailable: "+err.Error())
		return
	}
	if instance == nil {
		writeAPIError(w, r, http.StatusNotFound, "Process instance has no exporter records")
		return
	}

	detail := operateInstanceDetail{
		ZeebeProcessInstance: *instance,
		CaseID:               bc.ID,
		BusinessKey:          bc.CaseCode,
		CaseType:             bc.CaseType,
		CaseStatus:           bc.Status,
		Title:                bc.Title,
	}
	if bc.SLADueAt != nil {
		due := bc.SLADueAt.Format(time.RFC3339)
		detail.SLADueAt = &due
	}
	if counts, err := h.MonitoringIndex.OpenIncidentCounts(r.Context(), []int64{key}); err == nil {
		detail.OpenIncidents = counts[key]
	}
	writeJSON(w, r, http.StatusOK, detail)
}

func (h *WorkflowHandler) OperateInstanceElementInstances(w http.ResponseWriter, r *http.Request) {
	h.operateInstanceRead(w, r, "/element-instances", func(key int64) (any, error) {
		return h.MonitoringIndex.ListElementInstances(r.Context(), key)
	})
}

func (h *WorkflowHandler) OperateInstanceVariables(w http.ResponseWriter, r *http.Request) {
	h.operateInstanceRead(w, r, "/variables", func(key int64) (any, error) {
		return h.MonitoringIndex.ListVariables(r.Context(), key)
	})
}

func (h *WorkflowHandler) OperateInstanceJobs(w http.ResponseWriter, r *http.Request) {
	h.operateInstanceRead(w, r, "/jobs", func(key int64) (any, error) {
		return h.MonitoringIndex.ListJobs(r.Context(), key)
	})
}

// operateInstanceRead authorizes the instance through the tenant-scoped case
// registry before exposing any Zeebe exporter data.
func (h *WorkflowHandler) operateInstanceRead(w http.ResponseWriter, r *http.Request, suffix string, read func(int64) (any, error)) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	key, ok := parsePathInt64(r.URL.Path, "/api/workflow/operate/process-instances/", suffix)
	if !ok {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid process instance key")
		return
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), key)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope instance: "+err.Error())
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "Process instance not found")
		return
	}
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Runtime monitoring requires ZEEBE_ES_URL")
		return
	}
	result, err := read(key)
	if err != nil {
		writeAPIError(w, r, http.StatusBadGateway, "Runtime monitoring is unavailable: "+err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

func (h *WorkflowHandler) operateSearchIncidents(w http.ResponseWriter, r *http.Request) {
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		items := h.operateIncidentsFromTimeline(r.Context())
		rows := make([]operateIncidentRow, 0, len(items))
		for _, item := range items {
			rows = append(rows, operateIncidentRow{
				IncidentKey:        item.IncidentKey,
				ProcessInstanceKey: item.ProcessInstanceKey,
				BpmnProcessID:      item.BpmnProcessId,
				ElementID:          item.ElementId,
				ElementInstanceKey: item.ElementInstanceKey,
				JobKey:             item.JobKey,
				ErrorType:          item.ErrorType,
				ErrorMessage:       item.ErrorMessage,
				State:              item.State,
				CreatedAt:          item.CreatedAt,
			})
		}
		writeJSON(w, r, http.StatusOK, operateIncidentPage{Items: rows, Source: "database"})
		return
	}

	pageSize := clampOperatePageSize(int(queryInt64(r, "pageSize")))
	params := service.IncidentSearchParams{
		State:              strings.TrimSpace(r.URL.Query().Get("state")),
		ErrorType:          strings.TrimSpace(r.URL.Query().Get("errorType")),
		BpmnProcessID:      strings.TrimSpace(r.URL.Query().Get("bpmnProcessId")),
		ProcessInstanceKey: queryInt64(r, "processInstanceKey"),
		From:               queryTime(r, "from"),
		To:                 queryTime(r, "to"),
		PageSize:           pageSize,
		Cursor:             queryInt64(r, "cursor"),
	}

	rows := make([]operateIncidentRow, 0, pageSize)
	cursor := ""
	for attempt := 0; attempt < 4; attempt++ {
		items, next, err := h.MonitoringIndex.SearchIncidents(r.Context(), params)
		if err != nil {
			writeAPIError(w, r, http.StatusBadGateway, "Runtime monitoring is unavailable: "+err.Error())
			return
		}
		keys := make([]int64, 0, len(items))
		for _, item := range items {
			if item.ProcessInstanceKey > 0 {
				keys = append(keys, item.ProcessInstanceKey)
			}
		}
		cases, err := h.caseRepo.CasesByProcessInstanceKeys(r.Context(), keys)
		if err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope incidents: "+err.Error())
			return
		}
		for _, item := range items {
			bc, owned := cases[item.ProcessInstanceKey]
			if !owned {
				continue
			}
			rows = append(rows, operateIncidentRow{
				IncidentKey:        strconv.FormatInt(item.IncidentKey, 10),
				ProcessInstanceKey: strconv.FormatInt(item.ProcessInstanceKey, 10),
				BpmnProcessID:      item.BpmnProcessID,
				ElementID:          item.ElementID,
				ElementInstanceKey: formatKey(item.ElementInstanceKey),
				JobKey:             formatKey(item.JobKey),
				ErrorType:          item.ErrorType,
				ErrorMessage:       item.ErrorMessage,
				State:              item.State,
				CreatedAt:          item.CreationTime,
				CaseID:             bc.ID,
				BusinessKey:        bc.CaseCode,
			})
		}
		cursor = next
		if len(rows) >= pageSize || cursor == "" {
			break
		}
		params.Cursor = parseCursor(cursor)
		if params.Cursor <= 0 {
			cursor = ""
			break
		}
	}

	writeJSON(w, r, http.StatusOK, operateIncidentPage{Items: rows, NextCursor: cursor, Source: "zeebe-exporter"})
}

// ─── Query helpers ──────────────────────────────────────────────────────────────

func queryInt64(r *http.Request, name string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(name)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func queryTime(r *http.Request, name string) time.Time {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.UnixMilli(ms).UTC()
	}
	return time.Time{}
}

func parseCursor(cursor string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(cursor), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func clampOperatePageSize(size int) int {
	if size <= 0 {
		return 25
	}
	if size > 100 {
		return 100
	}
	return size
}

func formatKey(key int64) string {
	if key <= 0 {
		return ""
	}
	return strconv.FormatInt(key, 10)
}
