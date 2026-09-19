package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/handler"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

const aiTestSecret = "01234567890123456789012345678901"

// fakeAIStore implements handler.AIWorkflowStore without a database. It
// records the filters and the verified tenant metadata seen per call so tests
// can assert tenant scoping comes from the delegated headers, not from tool
// arguments.
type fakeAIStore struct {
	workItems []repository.WorkItem
	getItem   *repository.WorkItem
	cases     []repository.BusinessCase
	getCase   *repository.BusinessCase
	timeline  []repository.TimelineEvent

	lastWorkItemFilter repository.WorkItemFilter
	lastCaseFilter     repository.CaseListFilter
	lastTenantIDs      []string
	getWorkItemCalled  bool
	getCaseCalled      bool
	timelineCaseID     string
}

func (f *fakeAIStore) tenantFrom(ctx context.Context) string {
	return strings.TrimSpace(ardametadata.FromOutgoing(ctx).TenantID)
}

func (f *fakeAIStore) ListWorkItems(ctx context.Context, filter repository.WorkItemFilter) ([]repository.WorkItem, error) {
	f.lastWorkItemFilter = filter
	f.lastTenantIDs = append(f.lastTenantIDs, f.tenantFrom(ctx))
	return f.workItems, nil
}

func (f *fakeAIStore) GetWorkItem(ctx context.Context, id, userID string) (*repository.WorkItem, error) {
	f.getWorkItemCalled = true
	f.lastTenantIDs = append(f.lastTenantIDs, f.tenantFrom(ctx))
	return f.getItem, nil
}

func (f *fakeAIStore) ListCases(ctx context.Context, filter repository.CaseListFilter) ([]repository.BusinessCase, error) {
	f.lastCaseFilter = filter
	f.lastTenantIDs = append(f.lastTenantIDs, f.tenantFrom(ctx))
	return f.cases, nil
}

func (f *fakeAIStore) GetCase(ctx context.Context, id string) (*repository.BusinessCase, error) {
	f.getCaseCalled = true
	f.lastTenantIDs = append(f.lastTenantIDs, f.tenantFrom(ctx))
	return f.getCase, nil
}

func (f *fakeAIStore) ListTimeline(ctx context.Context, caseID string) ([]repository.TimelineEvent, error) {
	f.timelineCaseID = caseID
	f.lastTenantIDs = append(f.lastTenantIDs, f.tenantFrom(ctx))
	return f.timeline, nil
}

func newAIRouter(t *testing.T, store *fakeAIStore) http.Handler {
	t.Helper()
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	h := handler.NewWorkflowHandler(nil, nil, nil, nil, nil)
	h.SetAIStore(store)
	return NewRouter(h)
}

func aiSignedRequest(t *testing.T, target string, headers map[string]string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	token, err := identity.Issue(aiTestSecret, "ai-service", "workflow-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue ai-service token: %v", err)
	}
	req.Header.Set("X-Service-Auth", token)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Auth-Checked", "true")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestInternalAIService_MiddlewareAuth(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)

	next := internalAIService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	valid, err := identity.Issue(aiTestSecret, "ai-service", "workflow-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue valid token: %v", err)
	}
	wrongSource, err := identity.Issue(aiTestSecret, "crm-service", "workflow-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue wrong-source token: %v", err)
	}

	tests := []struct {
		name   string
		token  string
		status int
	}{
		{name: "missing token", status: http.StatusUnauthorized},
		{name: "valid ai-service token", token: valid, status: http.StatusNoContent},
		{name: "wrong source", token: wrongSource, status: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/ai/work-items", nil)
			if tt.token != "" {
				req.Header.Set("X-Service-Auth", tt.token)
			}
			res := httptest.NewRecorder()
			next.ServeHTTP(res, req)
			if res.Code != tt.status {
				t.Errorf("status = %d, want %d", res.Code, tt.status)
			}
		})
	}
}

// TestInternalAI_ListWorkItems_RedactsAndScopes covers the happy path: signed
// ai-service request + delegated headers returns the redacted allowlist and
// the verified tenant comes from X-Tenant-Id, never from tenant_id args.
func TestInternalAI_ListWorkItems_RedactsAndScopes(t *testing.T) {
	store := &fakeAIStore{workItems: []repository.WorkItem{{
		ID:                 "wi-1",
		CaseID:             "case-1",
		TenantID:           "tenant-1",
		CaseCode:           "HD-2026-0001",
		CaseType:           "FINANCE_INCOMING_TRANSACTION",
		PrimaryObjectType:  "finance_transaction",
		PrimaryObjectID:    "txn-1",
		ProcessInstanceKey: func() *int64 { v := int64(2251799813685001); return &v }(),
		JobKey:             func() *int64 { v := int64(2251799813685999); return &v }(),
		TaskType:           "workflow.finance_incoming_classify",
		StepCode:           "UT_Classify",
		Title:              "Phân loại giao dịch đến",
		Description:        "Mô tả nội bộ",
		Summary:            "Tóm tắt nội bộ",
		Status:             repository.TaskStatusReady,
		TransactionStatus:  "SUBMITTED",
		CreatedBy:          "maker-1",
		CandidateRole:      "FINANCE_TXN_MAKER",
		CandidateUsers:     []string{"clerk-1", "clerk-2"},
		CandidateGroupID:   "grp-1",
		CandidateOrgUnitID: "org-1",
		AssignedTo:         "user-1",
		PreviousAssignedTo: "prev-1",
		ClaimExpiresAt:     func() *time.Time { v := time.Now().Add(time.Hour); return &v }(),
		CreatedAt:          time.Now().Add(-time.Hour),
		UpdatedAt:          time.Now(),
	}}}
	router := newAIRouter(t, store)

	// tenant_id in the query must be ignored: scoping comes from the header.
	req := aiSignedRequest(t, "/internal/ai/work-items?tenant_id=tenant-evil&direction=INCOMING&per_page=1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}

	var envelope struct {
		Result struct {
			Items []map[string]any `json:"items"`
			Page  int              `json:"page"`
			Total int              `json:"total"`
		} `json:"result"`
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success {
		t.Error("envelope success = false")
	}
	if len(envelope.Result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(envelope.Result.Items))
	}
	item := envelope.Result.Items[0]
	if item["id"] != "wi-1" || item["caseCode"] != "HD-2026-0001" || item["title"] != "Phân loại giao dịch đến" {
		t.Errorf("allowlisted fields wrong: %v", item)
	}
	if canClaim, _ := item["canClaim"].(bool); canClaim {
		t.Error("canClaim should be false: item is assigned to the user")
	}
	if canOpen, _ := item["canOpen"].(bool); !canOpen {
		t.Error("canOpen should be true: item is assigned to the delegated user")
	}

	// Sensitive/internal fields must never appear anywhere in the body.
	body := res.Body.String()
	for _, banned := range []string{
		"tenantId", "description", "summary", "candidateUsers", "candidateGroupId",
		"candidateOrgUnitId", "assignedTo", "createdBy", "previousAssignedTo",
		"processInstanceKey", "jobKey", "claimExpiresAt", "Avatar", "candidateRole",
	} {
		if strings.Contains(body, banned) {
			t.Errorf("response body leaks %q: %s", banned, body)
		}
	}

	// Tenant scope must come from the delegated header.
	if len(store.lastTenantIDs) == 0 || store.lastTenantIDs[0] != "tenant-1" {
		t.Errorf("verified tenant = %v, want [tenant-1]", store.lastTenantIDs)
	}
	if store.lastWorkItemFilter.Direction != "INCOMING" {
		t.Errorf("direction = %q, want INCOMING", store.lastWorkItemFilter.Direction)
	}
}

// TestInternalAI_ListWorkItems_RejectsSearchDirection: the AI surface serves
// the work queues only, no search/ALL direction.
func TestInternalAI_ListWorkItems_RejectsSearchDirection(t *testing.T) {
	store := &fakeAIStore{}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/work-items?direction=ALL", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if len(store.lastTenantIDs) != 0 {
		t.Error("repo must not be called for an invalid direction")
	}
}

// TestInternalAI_ListWorkItems_IncomingVisibility: an incoming item held by
// another user's candidate role and neither assigned to nor created by the
// delegated user is filtered out of the AI response.
func TestInternalAI_ListWorkItems_IncomingVisibility(t *testing.T) {
	store := &fakeAIStore{workItems: []repository.WorkItem{
		{ID: "wi-mine", CaseType: "FINANCE_INCOMING_TRANSACTION", AssignedTo: "user-1", Status: repository.TaskStatusClaimed, CandidateRole: "FINANCE_TXN_CLASSIFIER"},
		{ID: "wi-role", CaseType: "FINANCE_INCOMING_TRANSACTION", Status: repository.TaskStatusReady, CandidateRole: "FINANCE_TXN_CLASSIFIER"},
		{ID: "wi-other", CaseType: "FINANCE_INCOMING_TRANSACTION", Status: repository.TaskStatusReady, CandidateRole: "FINANCE_TXN_CHECKER"},
	}}
	router := newAIRouter(t, store)

	// No X-Roles/X-Permissions headers: only the assigned item is visible.
	req := aiSignedRequest(t, "/internal/ai/work-items", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var envelope struct {
		Result struct {
			Items []map[string]any `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Result.Items) != 1 || envelope.Result.Items[0]["id"] != "wi-mine" {
		t.Fatalf("items = %v, want only wi-mine", envelope.Result.Items)
	}
}

// TestInternalAI_ListWorkItems_RequiresDelegatedTenant: a signed request
// without the delegated tenant scope is rejected (workflow router requires a
// verified tenant for every non-health route).
func TestInternalAI_ListWorkItems_RequiresDelegatedTenant(t *testing.T) {
	store := &fakeAIStore{}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/work-items", map[string]string{
		"X-Tenant-Id": "",
	})
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", res.Code, res.Body.String())
	}
	if len(store.lastTenantIDs) != 0 {
		t.Error("repo must not be called without a verified tenant")
	}
}

// TestInternalAI_ListWorkItems_Paginates checks page/per_page slicing over
// the fetched window.
func TestInternalAI_ListWorkItems_Paginates(t *testing.T) {
	items := make([]repository.WorkItem, 3)
	for i := range items {
		items[i] = repository.WorkItem{
			ID:            "wi-" + string(rune('a'+i)),
			CaseType:      "FINANCE_INCOMING_TRANSACTION",
			Status:        repository.TaskStatusClaimed,
			AssignedTo:    "user-1",
			CandidateRole: "FINANCE_TXN_CLASSIFIER",
		}
	}
	store := &fakeAIStore{workItems: items}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/work-items?page=2&per_page=1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var envelope struct {
		Result struct {
			Items   []map[string]any `json:"items"`
			Page    int              `json:"page"`
			PerPage int              `json:"per_page"`
			Total   int              `json:"total"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Result.Page != 2 || envelope.Result.PerPage != 1 {
		t.Errorf("page/per_page = %d/%d, want 2/1", envelope.Result.Page, envelope.Result.PerPage)
	}
	if len(envelope.Result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(envelope.Result.Items))
	}
}

func TestInternalAI_GetWorkItem_Redacts(t *testing.T) {
	item := &repository.WorkItem{
		ID:                 "wi-1",
		TenantID:           "tenant-1",
		CaseCode:           "HD-2026-0001",
		CaseType:           "CUSTOMER_REGISTRATION",
		Title:              "Phê duyệt hồ sơ khách hàng",
		Description:        "Mô tả nội bộ",
		Summary:            "Tóm tắt nội bộ",
		CreatedBy:          "maker-1",
		AssignedTo:         "clerk-1",
		ProcessInstanceKey: func() *int64 { v := int64(42); return &v }(),
		JobKey:             func() *int64 { v := int64(43); return &v }(),
		Status:             repository.TaskStatusClaimed,
	}
	store := &fakeAIStore{getItem: item}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/work-items/wi-1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, `"id":"wi-1"`) || !strings.Contains(body, "Phê duyệt hồ sơ khách hàng") {
		t.Errorf("allowlisted fields missing: %s", body)
	}
	for _, banned := range []string{"tenantId", "description", "summary", "assignedTo", "createdBy", "processInstanceKey", "jobKey"} {
		if strings.Contains(body, banned) {
			t.Errorf("response body leaks %q", banned)
		}
	}
}

func TestInternalAI_GetWorkItem_NotFound(t *testing.T) {
	store := &fakeAIStore{}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/work-items/missing", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestInternalAI_ListCases_Redacts(t *testing.T) {
	store := &fakeAIStore{cases: []repository.BusinessCase{{
		ID:                 "case-1",
		TenantID:           "tenant-1",
		CaseCode:           "HD-2026-0001",
		CaseType:           "FINANCE_INCOMING_TRANSACTION",
		Title:              "Giao dịch đến 1",
		Status:             "IN_REVIEW",
		CreatedBy:          "maker-1",
		ProcessInstanceKey: func() *int64 { v := int64(99); return &v }(),
		BpmnProcessID:      func() *string { v := "finance-incoming-v1"; return &v }(),
		BpmnVersion:        func() *int { v := 3; return &v }(),
	}}}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/cases?keyword=giao%20d%E1%BB%8Bch&status=in_review", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var envelope struct {
		Result struct {
			Items []map[string]any `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(envelope.Result.Items))
	}
	body := res.Body.String()
	if !strings.Contains(body, `"caseCode":"HD-2026-0001"`) {
		t.Errorf("allowlisted fields missing: %s", body)
	}
	for _, banned := range []string{"tenantId", "processInstanceKey", "bpmnProcessId", "bpmnVersion", "createdBy", "assignedTo", "candidateRole", "domainService", "primaryObjectId"} {
		if strings.Contains(body, banned) {
			t.Errorf("response body leaks %q", banned)
		}
	}
	if store.lastCaseFilter.Keyword != "giao dịch" {
		t.Errorf("keyword = %q, want 'giao dịch'", store.lastCaseFilter.Keyword)
	}
	if store.lastCaseFilter.Status != "IN_REVIEW" {
		t.Errorf("status = %q, want IN_REVIEW", store.lastCaseFilter.Status)
	}
}

func TestInternalAI_GetCase_Redacts(t *testing.T) {
	store := &fakeAIStore{getCase: &repository.BusinessCase{
		ID:        "case-1",
		TenantID:  "tenant-1",
		CaseCode:  "HD-2026-0001",
		CaseType:  "CUSTOMER_REGISTRATION",
		Title:     "Đăng ký khách hàng",
		Status:    "IN_REVIEW",
		CreatedBy: "maker-1",
	}}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/cases/case-1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, `"caseCode":"HD-2026-0001"`) {
		t.Errorf("allowlisted fields missing: %s", body)
	}
	for _, banned := range []string{"tenantId", "processInstanceKey", "bpmnProcessId", "assignedTo", "createdBy", "candidateRole"} {
		if strings.Contains(body, banned) {
			t.Errorf("response body leaks %q", banned)
		}
	}
	if !store.getCaseCalled {
		t.Error("GetCase was not called")
	}
}

func TestInternalAI_CaseTimeline_Redacts(t *testing.T) {
	from := "SUBMITTED"
	actor := "maker-1"
	store := &fakeAIStore{timeline: []repository.TimelineEvent{{
		ID:         7,
		CaseID:     "case-1",
		EventType:  "STATUS_CHANGED",
		FromStatus: &from,
		ToStatus:   &from,
		Actor:      &actor,
		Note:       "Ghi chú nội bộ",
		Data:       json.RawMessage(`{"secret":"internal"}`),
		CreatedAt:  time.Now().Add(-time.Hour),
	}}}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/cases/case-1/timeline", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, `"eventType":"STATUS_CHANGED"`) {
		t.Errorf("allowlisted fields missing: %s", body)
	}
	for _, banned := range []string{"secret", "actor", "note", "fromStatus", "toStatus", `"data"`, "caseId", "maker-1"} {
		if strings.Contains(body, banned) {
			t.Errorf("response body leaks %q", banned)
		}
	}
	if store.timelineCaseID != "case-1" {
		t.Errorf("timeline caseID = %q, want case-1", store.timelineCaseID)
	}
}

// TestInternalAI_TenantIgnoredFromArgs: a malicious tenant_id argument never
// changes the verified tenant handed to the repository.
func TestInternalAI_TenantIgnoredFromArgs(t *testing.T) {
	store := &fakeAIStore{}
	router := newAIRouter(t, store)

	req := aiSignedRequest(t, "/internal/ai/cases?tenant_id=tenant-evil", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if len(store.lastTenantIDs) != 1 || store.lastTenantIDs[0] != "tenant-1" {
		t.Errorf("verified tenant = %v, want [tenant-1]", store.lastTenantIDs)
	}
}
