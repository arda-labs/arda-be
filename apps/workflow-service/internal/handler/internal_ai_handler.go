package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// AIWorkflowStore is the read slice of the case/work-item store the internal
// AI surface needs. *repository.CaseRepository satisfies it; the transport
// tests inject a fake through WorkflowHandler.SetAIStore.
type AIWorkflowStore interface {
	ListWorkItems(ctx context.Context, f repository.WorkItemFilter) ([]repository.WorkItem, error)
	GetWorkItem(ctx context.Context, id, userID string) (*repository.WorkItem, error)
	ListCases(ctx context.Context, f repository.CaseListFilter) ([]repository.BusinessCase, error)
	GetCase(ctx context.Context, id string) (*repository.BusinessCase, error)
	ListTimeline(ctx context.Context, caseID string) ([]repository.TimelineEvent, error)
}

// aiStore returns the store backing the AI surface: the injected override
// when present, otherwise the production CaseRepository.
func (h *WorkflowHandler) aiStore() AIWorkflowStore {
	if h.aiStoreOverride != nil {
		return h.aiStoreOverride
	}
	return h.caseRepo
}

// SetAIStore overrides the store behind the internal AI surface (tests only;
// production leaves it nil so the real CaseRepository is used).
func (h *WorkflowHandler) SetAIStore(store AIWorkflowStore) {
	h.aiStoreOverride = store
}

const (
	aiDefaultPerPage   = 10
	aiMaxPerPage       = 20
	aiMaxWorkItemID    = 128
	aiMaxKeywordLen    = 128
	aiMaxFetchFromRepo = 200 // repository.ListWorkItems/ListCases clamp here
)

// aiWorkItem is the redacted work-item shape exposed to the AI SDK. Internal
// linkage and sensitive fields (tenant_id, description, summary, candidate
// users/groups/org units, assignedTo/createdBy identity, previous assignee,
// process/job keys, claim expiry, avatars) are dropped here; the response
// allowlist in contracts/ai-internal/workflow-v1.json drops them again as
// defense in depth.
type aiWorkItem struct {
	ID                string     `json:"id"`
	CaseCode          string     `json:"caseCode,omitempty"`
	CaseType          string     `json:"caseType"`
	TaskType          string     `json:"taskType"`
	StepCode          string     `json:"stepCode,omitempty"`
	Title             string     `json:"title"`
	Status            string     `json:"status"`
	TransactionStatus string     `json:"transactionStatus,omitempty"`
	SLAStatus         string     `json:"slaStatus,omitempty"`
	SLADueAt          *time.Time `json:"slaDueAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	CanClaim          bool       `json:"canClaim"`
	CanOpen           bool       `json:"canOpen"`
	CanReassign       bool       `json:"canReassign"`
}

// aiBusinessCase is the redacted case shape exposed to the AI SDK.
type aiBusinessCase struct {
	ID        string    `json:"id"`
	CaseCode  string    `json:"caseCode,omitempty"`
	CaseType  string    `json:"caseType"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// aiTimelineEvent is the redacted timeline shape exposed to the AI SDK. The
// raw `data` JSON blob, actor identity and free-text note are dropped.
type aiTimelineEvent struct {
	ID        int64     `json:"id"`
	EventType string    `json:"eventType"`
	CreatedAt time.Time `json:"createdAt"`
}

// requireAIDelegate enforces that a verified tenant scope arrived through the
// delegated subject headers (X-Tenant-Id, forwarded by ai-service). Tenant
// identity NEVER comes from tool arguments — a tenant_id query parameter is
// ignored entirely.
func requireAIDelegate(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("X-Tenant-Id")) == "" {
		writeAPIError(w, r, http.StatusForbidden, "verified tenant scope is required")
		return false
	}
	return true
}

// aiListPage parses page/per_page for the AI surface: per_page is clamped to
// 1-20 (default 10), page is at least 1.
func aiListPage(q map[string][]string) (int, int) {
	page := atoiDefault(first(q, "page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := atoiDefault(first(q, "per_page"), aiDefaultPerPage)
	if perPage < 1 {
		perPage = aiDefaultPerPage
	}
	if perPage > aiMaxPerPage {
		perPage = aiMaxPerPage
	}
	return page, perPage
}

// aiFetchLimit converts page*perPage into the single repo limit clause
// (repository clamps 1-200), so in-memory page slicing below the fetch limit
// stays correct.
func aiFetchLimit(page, perPage int) int {
	limit := page * perPage
	if limit < perPage {
		limit = perPage
	}
	if limit > aiMaxFetchFromRepo {
		limit = aiMaxFetchFromRepo
	}
	return limit
}

func first(values map[string][]string, key string) string {
	if v, ok := values[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func atoiDefault(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

// writeAIListPage slices the fetched (newest-first) items to the requested
// page and writes the canonical result envelope. The repo layer has no offset
// paging, so totals reflect the fetched window (newest aiMaxFetch items).
func writeAIListPage(w http.ResponseWriter, r *http.Request, page, perPage int, items []aiWorkItem) {
	total := len(items)
	start := (page - 1) * perPage
	var paged []aiWorkItem
	if start < total {
		end := start + perPage
		if end > total {
			end = total
		}
		paged = items[start:end]
	}
	writeJSON(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, paged))
}

// aiWorkItemVisible reports whether the delegated user may see an incoming
// queue item: maker-track creator, current assignee, or a holder of the
// candidate role (header-based; role membership DB lookups stay on the
// browser surface).
func aiWorkItemVisible(r *http.Request, userID string, item repository.WorkItem) bool {
	makerTrack := userID != "" && item.CreatedBy == userID && repository.IsMakerTrackCaseType(item.CaseType)
	if item.AssignedTo != "" {
		return item.AssignedTo == userID || makerTrack
	}
	return makerTrack || userCanClaimCandidateRole(r, item.CandidateRole)
}

// aiWorkItemView redacts a single WorkItem to the AI allowlist.
func aiWorkItemView(r *http.Request, userID string, item repository.WorkItem) aiWorkItem {
	roleOK := userCanClaimCandidateRole(r, item.CandidateRole)
	makerTrack := userID != "" && item.CreatedBy == userID && repository.IsMakerTrackCaseType(item.CaseType)
	return aiWorkItem{
		ID:                item.ID,
		CaseCode:          item.CaseCode,
		CaseType:          item.CaseType,
		TaskType:          item.TaskType,
		StepCode:          item.StepCode,
		Title:             item.Title,
		Status:            item.Status,
		TransactionStatus: item.TransactionStatus,
		SLAStatus:         item.SLAStatus,
		SLADueAt:          item.SLADueAt,
		CreatedAt:         item.CreatedAt,
		UpdatedAt:         item.UpdatedAt,
		CanClaim:          item.Status == repository.TaskStatusReady && item.AssignedTo == "" && roleOK,
		CanOpen:           makerTrack || (item.AssignedTo != "" && item.AssignedTo == userID) || (item.AssignedTo == "" && roleOK),
		CanReassign:       item.CanReassign,
	}
}

// InternalAIListWorkItems serves GET /internal/ai/work-items for ai-service.
// The signed caller assertion and delegated subject headers are verified by
// the router's internalAIService middleware; tenant scoping is enforced again
// by the repository layer via ardametadata.FromOutgoing, and incoming items
// are additionally filtered to the delegated user (claimable/assigned/maker).
func (h *WorkflowHandler) InternalAIListWorkItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if !requireAIDelegate(w, r) {
		return
	}
	q := r.URL.Query()
	// AI surface serves the work queues only — no search/ALL direction.
	direction := strings.ToUpper(strings.TrimSpace(q.Get("direction")))
	if direction == "" {
		direction = "INCOMING"
	}
	if direction != "INCOMING" && direction != "OUTGOING" {
		writeAPIError(w, r, http.StatusBadRequest, "direction must be INCOMING or OUTGOING")
		return
	}
	page, perPage := aiListPage(q)
	userID := currentUserID(r)
	filter := repository.WorkItemFilter{
		Direction:         direction,
		TransactionStatus: strings.ToUpper(strings.TrimSpace(q.Get("status"))),
		UserID:            userID,
		Limit:             aiFetchLimit(page, perPage),
	}
	items, err := h.aiStore().ListWorkItems(r.Context(), filter)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work items: "+err.Error())
		return
	}
	redacted := make([]aiWorkItem, 0, len(items))
	for _, item := range items {
		if direction == "INCOMING" && !aiWorkItemVisible(r, userID, item) {
			continue
		}
		redacted = append(redacted, aiWorkItemView(r, userID, item))
	}
	writeAIListPage(w, r, page, perPage, redacted)
}

// InternalAIGetWorkItem serves GET /internal/ai/work-items/{itemId} for
// ai-service. Tenant scoping happens again inside GetWorkItem via the
// verified tenant metadata, so a tenant can never read another tenant's item.
func (h *WorkflowHandler) InternalAIGetWorkItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if !requireAIDelegate(w, r) {
		return
	}
	id := r.PathValue("itemId")
	if id == "" {
		writeAPIError(w, r, http.StatusNotFound, "work item not found")
		return
	}
	if len(id) > aiMaxWorkItemID {
		writeAPIError(w, r, http.StatusBadRequest, "work item id is too long")
		return
	}
	item, err := h.aiStore().GetWorkItem(r.Context(), id, currentUserID(r))
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query work item: "+err.Error())
		return
	}
	if item == nil {
		writeAPIError(w, r, http.StatusNotFound, "work item not found")
		return
	}
	writeJSON(w, r, http.StatusOK, aiWorkItemView(r, currentUserID(r), *item))
}

// InternalAIListCases serves GET /internal/ai/cases for ai-service.
func (h *WorkflowHandler) InternalAIListCases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if !requireAIDelegate(w, r) {
		return
	}
	q := r.URL.Query()
	keyword := strings.TrimSpace(q.Get("keyword"))
	if len(keyword) > aiMaxKeywordLen {
		writeAPIError(w, r, http.StatusBadRequest, "keyword is too long (max 128 chars)")
		return
	}
	page, perPage := aiListPage(q)
	filter := repository.CaseListFilter{
		Status:  strings.ToUpper(strings.TrimSpace(q.Get("status"))),
		Keyword: keyword,
		Limit:   aiFetchLimit(page, perPage),
	}
	cases, err := h.aiStore().ListCases(r.Context(), filter)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query cases: "+err.Error())
		return
	}
	view := make([]aiBusinessCase, 0, len(cases))
	for _, bc := range cases {
		view = append(view, aiBusinessCase{
			ID:        bc.ID,
			CaseCode:  bc.CaseCode,
			CaseType:  bc.CaseType,
			Status:    bc.Status,
			Title:     bc.Title,
			CreatedAt: bc.CreatedAt,
			UpdatedAt: bc.UpdatedAt,
		})
	}
	total := len(view)
	start := (page - 1) * perPage
	paged := []aiBusinessCase{}
	if start < total {
		end := start + perPage
		if end > total {
			end = total
		}
		paged = view[start:end]
	}
	writeJSON(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, paged))
}

// InternalAIGetCase serves GET /internal/ai/cases/{caseId} for ai-service.
func (h *WorkflowHandler) InternalAIGetCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if !requireAIDelegate(w, r) {
		return
	}
	id := r.PathValue("caseId")
	if id == "" {
		writeAPIError(w, r, http.StatusNotFound, "case not found")
		return
	}
	if len(id) > aiMaxWorkItemID {
		writeAPIError(w, r, http.StatusBadRequest, "case id is too long")
		return
	}
	bc, err := h.aiStore().GetCase(r.Context(), id)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query case: "+err.Error())
		return
	}
	if bc == nil {
		writeAPIError(w, r, http.StatusNotFound, "case not found")
		return
	}
	writeJSON(w, r, http.StatusOK, aiBusinessCase{
		ID:        bc.ID,
		CaseCode:  bc.CaseCode,
		CaseType:  bc.CaseType,
		Status:    bc.Status,
		Title:     bc.Title,
		CreatedAt: bc.CreatedAt,
		UpdatedAt: bc.UpdatedAt,
	})
}

// InternalAICaseTimeline serves GET /internal/ai/cases/{caseId}/timeline for
// ai-service. ListTimeline is tenant-scoped inside the repository; events are
// redacted to id/eventType/createdAt only.
func (h *WorkflowHandler) InternalAICaseTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if !requireAIDelegate(w, r) {
		return
	}
	id := r.PathValue("caseId")
	if id == "" {
		writeAPIError(w, r, http.StatusNotFound, "case not found")
		return
	}
	if len(id) > aiMaxWorkItemID {
		writeAPIError(w, r, http.StatusBadRequest, "case id is too long")
		return
	}
	events, err := h.aiStore().ListTimeline(r.Context(), id)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "Failed to query timeline: "+err.Error())
		return
	}
	redacted := make([]aiTimelineEvent, 0, len(events))
	for _, ev := range events {
		redacted = append(redacted, aiTimelineEvent{
			ID:        ev.ID,
			EventType: ev.EventType,
			CreatedAt: ev.CreatedAt,
		})
	}
	perPage := len(redacted)
	if perPage == 0 {
		perPage = 1
	}
	writeJSON(w, r, http.StatusOK, ardahttp.NewListResponse(1, perPage, len(redacted), redacted))
}
