package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

func incidentsFromTimeline(events []repository.TimelineEvent) []service.ProcessIncidentSnapshot {
	out := []service.ProcessIncidentSnapshot{}
	for _, event := range events {
		if event.EventType != "JOB_FAILED" {
			continue
		}
		incident, ok := parseJobFailureNote(event.Note)
		if !ok {
			continue
		}
		incident.CreatedAt = event.CreatedAt
		out = append(out, incident)
	}
	return out
}

func parseJobFailureNote(note string) (service.ProcessIncidentSnapshot, bool) {
	out := service.ProcessIncidentSnapshot{}
	if strings.TrimSpace(note) == "" {
		return out, false
	}
	for _, part := range strings.Split(note, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		switch key {
		case "jobKey":
			out.JobKey = value
		case "jobType":
			out.JobType = value
		case "elementId":
			out.ElementID = value
		case "retries":
			retries, _ := strconv.Atoi(value)
			out.Retries = retries
		case "error":
			decoded, err := url.QueryUnescape(value)
			if err != nil {
				out.ErrorMessage = value
			} else {
				out.ErrorMessage = decoded
			}
		}
	}
	return out, out.JobKey != ""
}

func activeElementID(bc *repository.BusinessCase, jobs []service.ProcessJobSnapshot, incidents []service.ProcessIncidentSnapshot) string {
	if len(jobs) > 0 && jobs[0].ElementID != "" {
		return jobs[0].ElementID
	}
	if len(incidents) > 0 && incidents[len(incidents)-1].ElementID != "" {
		return incidents[len(incidents)-1].ElementID
	}
	if bc == nil {
		return ""
	}
	step := strings.TrimSpace(bc.CurrentStep)
	if step != "" && step != "submitted" {
		return step
	}
	return ""
}

func (h *WorkflowHandler) retryJob(w http.ResponseWriter, r *http.Request, jobKey int64) {
	if h.zeebeSvc == nil {
		http.Error(w, "Zeebe service is not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Retries int32 `json:"retries"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.zeebeSvc.RetryJob(r.Context(), jobKey, req.Retries); err != nil {
		http.Error(w, "Failed to retry job: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "retried",
		"jobKey":  strconv.FormatInt(jobKey, 10),
		"retries": req.Retries,
	})
}

func (h *WorkflowHandler) retryProcessServiceJobs(w http.ResponseWriter, r *http.Request, processInstanceKey int64) {
	if h.zeebeSvc == nil {
		http.Error(w, "Zeebe service is not configured", http.StatusServiceUnavailable)
		return
	}
	bc, err := h.caseRepo.GetCaseByProcessInstanceKey(r.Context(), processInstanceKey)
	if err != nil {
		http.Error(w, "Failed to query case: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	retried := []string{}
	var timeline []repository.TimelineEvent
	if bc != nil {
		timeline, _ = h.caseRepo.ListTimeline(ctx, bc.ID)
	}
	for _, incident := range incidentsFromTimeline(timeline) {
		if !strings.HasPrefix(incident.JobType, "crm.") && !strings.HasPrefix(incident.JobType, "notification.") {
			continue
		}
		jobKey, err := strconv.ParseInt(incident.JobKey, 10, 64)
		if err != nil || jobKey <= 0 {
			continue
		}
		if err := h.zeebeSvc.RetryJob(ctx, jobKey, 3); err != nil {
			http.Error(w, fmt.Sprintf("Failed to retry job %d: %v", jobKey, err), http.StatusBadGateway)
			return
		}
		retried = append(retried, incident.JobKey)
	}
	if len(retried) == 0 {
		caseType := ""
		currentStep := ""
		if bc != nil {
			caseType = bc.CaseType
			currentStep = bc.CurrentStep
		}
		jobs, err := h.zeebeSvc.FindProcessJobsForCase(ctx, processInstanceKey, caseType, currentStep)
		if err != nil && len(jobs) == 0 {
			http.Error(w, "No incidents to retry and job scan failed: "+err.Error(), http.StatusNotFound)
			return
		}
		for _, job := range jobs {
			if !strings.HasPrefix(job.JobType, "crm.") && !strings.HasPrefix(job.JobType, "notification.") {
				continue
			}
			if job.Retries > 0 {
				continue
			}
			if err := h.zeebeSvc.RetryJob(ctx, job.JobKey, 3); err != nil {
				http.Error(w, "Failed to retry job: "+err.Error(), http.StatusBadGateway)
				return
			}
			retried = append(retried, strconv.FormatInt(job.JobKey, 10))
		}
	}
	if len(retried) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "noop",
			"message": "Không tìm thấy service job incident để retry — kiểm tra tab Jobs hoặc log workflow-service.",
			"retried": retried,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "retried",
		"retried": retried,
	})
}

func jobPath(path string) (int64, string) {
	const prefix = "/api/workflow/jobs/"
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return 0, ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return 0, ""
	}
	jobKey, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, ""
	}
	return jobKey, parts[1]
}
