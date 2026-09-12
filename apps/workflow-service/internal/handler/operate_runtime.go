package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
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

type operateJobRow struct {
	service.ZeebeJob
	CaseID      string `json:"caseId,omitempty"`
	BusinessKey string `json:"businessKey,omitempty"`
}

type operateJobPage struct {
	Items      []operateJobRow `json:"items"`
	NextCursor string          `json:"nextCursor,omitempty"`
	Source     string          `json:"source"`
}

func (h *WorkflowHandler) operateSearchJobs(w http.ResponseWriter, r *http.Request) {
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		writeJSON(w, r, http.StatusOK, operateJobPage{Items: []operateJobRow{}, Source: "unavailable"})
		return
	}

	pageSize := clampOperatePageSize(int(queryInt64(r, "pageSize")))
	params := service.JobSearchParams{
		State:              strings.TrimSpace(r.URL.Query().Get("state")),
		Type:               strings.TrimSpace(r.URL.Query().Get("type")),
		BpmnProcessID:      strings.TrimSpace(r.URL.Query().Get("bpmnProcessId")),
		ProcessInstanceKey: queryInt64(r, "processInstanceKey"),
		ElementID:          strings.TrimSpace(r.URL.Query().Get("elementId")),
		PageSize:           pageSize,
		Cursor:             queryInt64(r, "cursor"),
	}

	rows := make([]operateJobRow, 0, pageSize)
	cursor := ""
	for attempt := 0; attempt < 4; attempt++ {
		items, next, err := h.MonitoringIndex.SearchJobs(r.Context(), params)
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
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope jobs: "+err.Error())
			return
		}
		for _, item := range items {
			key, _ := strconv.ParseInt(item.ProcessInstanceKey, 10, 64)
			bc, owned := cases[key]
			if !owned {
				continue
			}
			rows = append(rows, operateJobRow{
				ZeebeJob:    item,
				CaseID:      bc.ID,
				BusinessKey: bc.CaseCode,
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

	writeJSON(w, r, http.StatusOK, operateJobPage{Items: rows, NextCursor: cursor, Source: "zeebe-exporter"})
}

func (h *WorkflowHandler) OperateInstanceHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	key, ok := parsePathInt64(r.URL.Path, "/api/workflow/operate/process-instances/", "/history")
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
	events, next, err := h.MonitoringIndex.ListHistory(r.Context(), key, queryInt64(r, "cursor"), int(queryInt64(r, "limit")))
	if err != nil {
		writeAPIError(w, r, http.StatusBadGateway, "Runtime monitoring is unavailable: "+err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": events, "nextCursor": next})
}

// ─── Runtime summary ────────────────────────────────────────────────────────────

const (
	summaryInstanceScanPages = 20 // 100 items per page
	summaryIncidentScanPages = 10
	summaryJobScanPages      = 10
	summaryBreakdownLimit    = 5
)

type operateSummaryCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type operateSummary struct {
	ActiveInstances    int                   `json:"activeInstances"`
	OpenIncidents      int                   `json:"openIncidents"`
	FailedJobs         int                   `json:"failedJobs"`
	Truncated          bool                  `json:"truncated"`
	IncidentsByType    []operateSummaryCount `json:"incidentsByType"`
	FailedJobsByType   []operateSummaryCount `json:"failedJobsByType"`
	InstancesByProcess []operateSummaryCount `json:"instancesByProcess"`
}

// OperateSummary aggregates tenant-scoped runtime counters. Counts come from
// bounded scans of the exporter read model (no cross-tenant aggregations on
// Zeebe's shared <default> tenant), so heavy deployments see truncated=true.
func (h *WorkflowHandler) OperateSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	summary := operateSummary{
		IncidentsByType:    []operateSummaryCount{},
		FailedJobsByType:   []operateSummaryCount{},
		InstancesByProcess: []operateSummaryCount{},
	}
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		writeJSON(w, r, http.StatusOK, summary)
		return
	}

	summary.ActiveInstances, summary.InstancesByProcess, summary.Truncated = h.summaryActiveInstances(r)
	incidents, incidentsByType, truncatedIncidents := h.summaryOpenIncidents(r)
	summary.OpenIncidents = incidents
	summary.IncidentsByType = incidentsByType
	jobs, jobsByType, truncatedJobs := h.summaryFailedJobs(r)
	summary.FailedJobs = jobs
	summary.FailedJobsByType = jobsByType
	summary.Truncated = summary.Truncated || truncatedIncidents || truncatedJobs

	writeJSON(w, r, http.StatusOK, summary)
}

func (h *WorkflowHandler) summaryActiveInstances(r *http.Request) (int, []operateSummaryCount, bool) {
	counts := map[string]int{}
	total := 0
	cursor := ""
	truncated := false
	params := service.ProcessInstanceSearchParams{State: "ACTIVE", PageSize: 100}
	for page := 0; page < summaryInstanceScanPages; page++ {
		params.Cursor = parseCursor(cursor)
		items, next, err := h.MonitoringIndex.SearchProcessInstances(r.Context(), params)
		if err != nil {
			break
		}
		total += h.countOwnedInstances(r, items, counts)
		if next == "" {
			break
		}
		cursor = next
		if page == summaryInstanceScanPages-1 {
			truncated = true
		}
	}
	return total, topSummaryCounts(counts), truncated
}

func (h *WorkflowHandler) summaryOpenIncidents(r *http.Request) (int, []operateSummaryCount, bool) {
	counts := map[string]int{}
	total := 0
	cursor := ""
	truncated := false
	params := service.IncidentSearchParams{State: "CREATED", PageSize: 100}
	for page := 0; page < summaryIncidentScanPages; page++ {
		params.Cursor = parseCursor(cursor)
		items, next, err := h.MonitoringIndex.SearchIncidents(r.Context(), params)
		if err != nil {
			break
		}
		keys := make([]int64, 0, len(items))
		for _, item := range items {
			if item.ProcessInstanceKey > 0 {
				keys = append(keys, item.ProcessInstanceKey)
			}
		}
		cases, err := h.caseRepo.CasesByProcessInstanceKeys(r.Context(), keys)
		if err != nil {
			break
		}
		for _, item := range items {
			if _, owned := cases[item.ProcessInstanceKey]; !owned {
				continue
			}
			total++
			label := item.ErrorType
			if label == "" {
				label = "UNKNOWN"
			}
			counts[label]++
		}
		if next == "" {
			break
		}
		cursor = next
		if page == summaryIncidentScanPages-1 {
			truncated = true
		}
	}
	return total, topSummaryCounts(counts), truncated
}

func (h *WorkflowHandler) summaryFailedJobs(r *http.Request) (int, []operateSummaryCount, bool) {
	counts := map[string]int{}
	total := 0
	cursor := ""
	truncated := false
	params := service.JobSearchParams{State: "FAILED", PageSize: 100}
	for page := 0; page < summaryJobScanPages; page++ {
		params.Cursor = parseCursor(cursor)
		items, next, err := h.MonitoringIndex.SearchJobs(r.Context(), params)
		if err != nil {
			break
		}
		keys := make([]int64, 0, len(items))
		for _, item := range items {
			if key, err := strconv.ParseInt(item.ProcessInstanceKey, 10, 64); err == nil {
				keys = append(keys, key)
			}
		}
		cases, err := h.caseRepo.CasesByProcessInstanceKeys(r.Context(), keys)
		if err != nil {
			break
		}
		for _, item := range items {
			key, _ := strconv.ParseInt(item.ProcessInstanceKey, 10, 64)
			if _, owned := cases[key]; !owned {
				continue
			}
			total++
			counts[item.Type]++
		}
		if next == "" {
			break
		}
		cursor = next
		if page == summaryJobScanPages-1 {
			truncated = true
		}
	}
	return total, topSummaryCounts(counts), truncated
}

func (h *WorkflowHandler) countOwnedInstances(r *http.Request, items []service.ZeebeProcessInstance, counts map[string]int) int {
	keys := make([]int64, 0, len(items))
	for _, item := range items {
		if key, err := strconv.ParseInt(item.ProcessInstanceKey, 10, 64); err == nil {
			keys = append(keys, key)
		}
	}
	cases, err := h.caseRepo.CasesByProcessInstanceKeys(r.Context(), keys)
	if err != nil {
		return 0
	}
	total := 0
	for _, item := range items {
		key, _ := strconv.ParseInt(item.ProcessInstanceKey, 10, 64)
		if _, owned := cases[key]; !owned {
			continue
		}
		total++
		counts[item.BpmnProcessID]++
	}
	return total
}

func topSummaryCounts(counts map[string]int) []operateSummaryCount {
	out := make([]operateSummaryCount, 0, len(counts))
	for label, count := range counts {
		out = append(out, operateSummaryCount{Label: label, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Label < out[j].Label
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > summaryBreakdownLimit {
		out = out[:summaryBreakdownLimit]
	}
	return out
}

// ─── Global user tasks ──────────────────────────────────────────────────────────

type operateUserTaskRow struct {
	UserTaskKey        string   `json:"userTaskKey"`
	ElementID          string   `json:"elementId,omitempty"`
	ElementInstanceKey string   `json:"elementInstanceKey,omitempty"`
	ProcessInstanceKey string   `json:"processInstanceKey"`
	BpmnProcessID      string   `json:"bpmnProcessId,omitempty"`
	State              string   `json:"state"`
	Assignee           string   `json:"assignee,omitempty"`
	CandidateGroups    []string `json:"candidateGroups,omitempty"`
	Priority           int      `json:"priority,omitempty"`
	DueDate            string   `json:"dueDate,omitempty"`
	FollowUpDate       string   `json:"followUpDate,omitempty"`
	CreatedAt          string   `json:"createdAt,omitempty"`
	CaseID             string   `json:"caseId,omitempty"`
	BusinessKey        string   `json:"businessKey,omitempty"`
}

type operateUserTaskPage struct {
	Items      []operateUserTaskRow `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
	Source     string               `json:"source"`
}

func (h *WorkflowHandler) OperateUserTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		writeJSON(w, r, http.StatusOK, operateUserTaskPage{Items: []operateUserTaskRow{}, Source: "unavailable"})
		return
	}

	pageSize := clampOperatePageSize(int(queryInt64(r, "pageSize")))
	params := service.UserTaskSearchParams{
		State:              strings.TrimSpace(r.URL.Query().Get("state")),
		Assignee:           strings.TrimSpace(r.URL.Query().Get("assignee")),
		CandidateGroup:     strings.TrimSpace(r.URL.Query().Get("candidateGroup")),
		BpmnProcessID:      strings.TrimSpace(r.URL.Query().Get("bpmnProcessId")),
		ProcessInstanceKey: queryInt64(r, "processInstanceKey"),
		ElementID:          strings.TrimSpace(r.URL.Query().Get("elementId")),
		PageSize:           pageSize,
		Cursor:             queryInt64(r, "cursor"),
	}

	rows := make([]operateUserTaskRow, 0, pageSize)
	cursor := ""
	for attempt := 0; attempt < 4; attempt++ {
		items, next, err := h.MonitoringIndex.SearchUserTasks(r.Context(), params)
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
			writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope user tasks: "+err.Error())
			return
		}
		for _, item := range items {
			bc, owned := cases[item.ProcessInstanceKey]
			if !owned {
				continue
			}
			rows = append(rows, operateUserTaskRow{
				UserTaskKey:        strconv.FormatInt(item.UserTaskKey, 10),
				ElementID:          item.ElementID,
				ElementInstanceKey: formatKey(item.ElementInstanceKey),
				ProcessInstanceKey: strconv.FormatInt(item.ProcessInstanceKey, 10),
				BpmnProcessID:      item.BpmnProcessID,
				State:              item.State,
				Assignee:           item.Assignee,
				CandidateGroups:    item.CandidateGroups,
				Priority:           item.Priority,
				DueDate:            item.DueDate,
				FollowUpDate:       item.FollowUpDate,
				CreatedAt:          item.CreatedAt,
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

	writeJSON(w, r, http.StatusOK, operateUserTaskPage{Items: rows, NextCursor: cursor, Source: "zeebe-exporter"})
}

func (h *WorkflowHandler) OperateUserTaskAssign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	key, ok := parsePathInt64(r.URL.Path, "/api/workflow/operate/user-tasks/", "/assign")
	if !ok {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid user task key")
		return
	}
	if h.MonitoringIndex == nil || !h.MonitoringIndex.Enabled() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Runtime monitoring requires ZEEBE_ES_URL")
		return
	}
	if h.zeebeRest == nil || !h.zeebeRest.Enabled() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "Zeebe REST client is not configured")
		return
	}

	var req struct {
		Assignee string `json:"assignee"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeAPIError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	assignee := strings.TrimSpace(req.Assignee)
	if assignee == "" {
		assignee = strings.TrimSpace(ardametadata.FromOutgoing(r.Context()).UserID)
	}
	if assignee == "" {
		writeAPIError(w, r, http.StatusBadRequest, "assignee is required")
		return
	}

	// Verify the task belongs to an instance owned by the caller tenant before
	// assigning an actor on the shared Zeebe cluster.
	task, err := h.MonitoringIndex.GetUserTask(r.Context(), key)
	if err != nil {
		writeAPIError(w, r, http.StatusBadGateway, "Runtime monitoring is unavailable: "+err.Error())
		return
	}
	if task == nil {
		writeAPIError(w, r, http.StatusNotFound, "User task not found")
		return
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), task.ProcessInstanceKey)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to scope user task: "+err.Error())
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "User task not found")
		return
	}

	if err := h.zeebeRest.AssignUserTask(r.Context(), key, assignee); err != nil {
		writeAPIError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{
		"status":      "assigned",
		"userTaskKey": strconv.FormatInt(key, 10),
		"assignee":    assignee,
	})
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
