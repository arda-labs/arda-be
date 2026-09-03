package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type fakeRunStore struct {
	started  repository.RunContext
	message  string
	finished bool
}

func (s *fakeRunStore) Start(_ context.Context, run repository.RunContext, message string) error {
	s.started = run
	s.message = message
	return nil
}

func (s *fakeRunStore) Finish(_ context.Context, run repository.RunContext, _ string, _ string) error {
	s.finished = run == s.started
	return nil
}

type fakeToolRunStore struct {
	fakeRunStore
	toolStarted     bool
	toolFinished    bool
	approvalCreated bool
	approvalDecided bool
}

func (s *fakeToolRunStore) StartTool(_ context.Context, _ repository.RunContext, _ string, _ int, _, _, _ string) (string, error) {
	s.toolStarted = true
	return "tool-execution-1", nil
}

func (s *fakeToolRunStore) FinishTool(_ context.Context, _, _, _, _ string) error {
	s.toolFinished = true
	return nil
}

func (s *fakeToolRunStore) CreateApprovalProposal(_ context.Context, _ repository.ApprovalProposal) (repository.ApprovalRecord, error) {
	s.approvalCreated = true
	return repository.ApprovalRecord{ID: "approval-1", Status: "PENDING", ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

func (s *fakeToolRunStore) DecideApproval(_ context.Context, _, _, _, _ string) (repository.ApprovalRecord, error) {
	s.approvalDecided = true
	return repository.ApprovalRecord{ID: "approval-1", Status: "APPROVED", ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

func (s *fakeToolRunStore) ListApprovals(_ context.Context, _, _ string, _, _ int) ([]repository.ApprovalDetail, error) {
	return []repository.ApprovalDetail{}, nil
}

type handlerTestTool struct{}

func (handlerTestTool) Definition() tools.Definition {
	return tools.Definition{Name: "test.read", Version: 1, Kind: "read", RequiredPermissions: []string{"crm.customer.read"}, Risk: "low"}
}

func (handlerTestTool) Execute(_ context.Context, _ tools.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{Data: json.RawMessage(`{"id":"customer-1"}`), Summary: "Customer A is ACTIVE."}, nil
}

func TestRunRequiresGatewayAuthContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"hello"}]}`))
	res := httptest.NewRecorder()
	NewRouter().ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestRunRequiresAssistantPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-Permissions", "crm.customer.read")
	res := httptest.NewRecorder()
	NewRouter().ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestRunStreamsDeterministicUIStreamParts(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-Permissions", "ai.assistant.use")
	res := httptest.NewRecorder()
	NewRouter().ServeHTTP(res, req)

	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status/content type = %d/%q", res.Code, res.Header().Get("Content-Type"))
	}
	var events []string
	scanner := bufio.NewScanner(strings.NewReader(res.Body.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, line)
		}
	}
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if len(events) != len(want) {
		t.Fatalf("event count = %d, want %d: %s", len(events), len(want), res.Body.String())
	}
	for i, event := range want {
		if !strings.Contains(events[i], `"type":"`+event+`"`) {
			t.Fatalf("event %d = %s, want type %s", i, events[i], event)
		}
	}
}

func TestRunPersistsServerResolvedOwnershipAndRedactsSecrets(t *testing.T) {
	store := &fakeRunStore{}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"use Bearer secret-token"}]}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-Permissions", "ai.assistant.use")
	res := httptest.NewRecorder()
	NewRouter(store).ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if store.started.TenantID != "tenant-1" || store.started.ActorUserID != "user-1" {
		t.Fatalf("store ownership = %#v", store.started)
	}
	if strings.Contains(store.message, "secret-token") || !store.finished {
		t.Fatalf("store message/finish = %q/%v", store.message, store.finished)
	}
}

func TestRunExecutesAllowlistedReadToolAndEmitsToolEvents(t *testing.T) {
	store := &fakeToolRunStore{}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r-tool-1","messages":[{"role":"user","content":"lookup"}],"tool":{"name":"test.read","arguments":{}}}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Org-Ids", "org-1")
	req.Header.Set("X-Org-Id", "org-1")
	req.Header.Set("X-Permissions", "ai.assistant.use,crm.customer.read")
	res := httptest.NewRecorder()
	NewRouterWithDependencies(store, tools.NewRegistry(handlerTestTool{})).ServeHTTP(res, req)

	if res.Code != http.StatusOK || !store.toolStarted || !store.toolFinished || !store.finished {
		t.Fatalf("status/tool lifecycle = %d/%v/%v/%v", res.Code, store.toolStarted, store.toolFinished, store.finished)
	}
	for _, event := range []string{"TOOL_CALL_START", "TOOL_CALL_END", "Customer A is ACTIVE.", "RUN_FINISHED"} {
		if !strings.Contains(res.Body.String(), event) {
			t.Fatalf("stream missing %q: %s", event, res.Body.String())
		}
	}
}

func TestApprovalProposalIsTypedAndFeatureFlagged(t *testing.T) {
	store := &fakeToolRunStore{}
	body := `{"threadId":"t-approval","runId":"r-approval","idempotencyKey":"idem-1","tool":{"name":"crm.customer.export.prepare","version":1,"arguments":{"customerId":"customer-1","format":"csv"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals", strings.NewReader(body))
	for key, value := range map[string]string{
		"X-Auth-Checked": "true", "X-User-Id": "user-1", "X-Tenant-Id": "tenant-1",
		"X-Permissions": "ai.assistant.use,ai.approval.propose,crm.customer.read",
	} {
		req.Header.Set(key, value)
	}
	res := httptest.NewRecorder()
	NewRouterWithOptions(store, nil, RouterOptions{EnableHITLProposals: true}).ServeHTTP(res, req)
	if res.Code != http.StatusCreated || !store.approvalCreated || !strings.Contains(res.Body.String(), `"status":"PENDING"`) {
		t.Fatalf("proposal response = %d/%v/%s", res.Code, store.approvalCreated, res.Body.String())
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/ai/approvals", strings.NewReader(`{"threadId":"t","runId":"r","idempotencyKey":"i","tool":{"name":"crm.customer.export.prepare","arguments":{"customerId":"c","format":"csv","secret":"no"}}}`))
	for key, value := range map[string]string{
		"X-Auth-Checked": "true", "X-User-Id": "user-1", "X-Tenant-Id": "tenant-1",
		"X-Permissions": "ai.assistant.use,ai.approval.propose,crm.customer.read",
	} {
		bad.Header.Set(key, value)
	}
	badResponse := httptest.NewRecorder()
	NewRouterWithOptions(store, nil, RouterOptions{EnableHITLProposals: true}).ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid proposal status = %d", badResponse.Code)
	}

	disabled := httptest.NewRecorder()
	NewRouterWithOptions(store, nil, RouterOptions{}).ServeHTTP(disabled, req)
	if disabled.Code != http.StatusNotFound {
		t.Fatalf("disabled proposal status = %d", disabled.Code)
	}
}

func TestApprovalDecisionRequiresIndependentApproverPermission(t *testing.T) {
	store := &fakeToolRunStore{}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/approval-1/decision", strings.NewReader(`{"decision":"approve"}`))
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "approver-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-Permissions", "ai.assistant.use,ai.approval.execute")
	res := httptest.NewRecorder()
	NewRouterWithOptions(store, nil, RouterOptions{EnableHITLProposals: true}).ServeHTTP(res, req)
	if res.Code != http.StatusOK || !store.approvalDecided || !strings.Contains(res.Body.String(), `"status":"APPROVED"`) {
		t.Fatalf("decision response = %d/%v/%s", res.Code, store.approvalDecided, res.Body.String())
	}
}

func TestServiceAuthMiddlewareAllowsHealthProbes(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()
	ServiceAuthMiddleware(NewRouter(), strings.Repeat("s", 32), true).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestServiceAuthMiddlewareRejectsMissingWorkloadIdentityOnApplicationRoutes(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	ServiceAuthMiddleware(NewRouter(), strings.Repeat("s", 32), true).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestListToolsEndpoint(t *testing.T) {
	options := RouterOptions{
		CatalogTools: []CatalogToolDTO{
			{MethodName: "crm.getCustomer", SDKPath: "arda.crm.getCustomer", Domain: "crm", Risk: "low"},
			{MethodName: "hrm.listEmployees", SDKPath: "arda.hrm.listEmployees", Domain: "hrm", Risk: "low"},
		},
	}
	router := NewRouterWithOptions(nil, nil, options)

	req := httptest.NewRequest(http.MethodGet, "/api/ai/tools", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "crm.getCustomer") {
		t.Fatalf("list tools failed: code = %d, body = %s", res.Code, res.Body.String())
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/api/ai/tools?domain=crm", nil)
	filterRes := httptest.NewRecorder()
	router.ServeHTTP(filterRes, filterReq)
	if filterRes.Code != http.StatusOK || !strings.Contains(filterRes.Body.String(), "crm.getCustomer") || strings.Contains(filterRes.Body.String(), "hrm.listEmployees") {
		t.Fatalf("filter tools failed: code = %d, body = %s", filterRes.Code, filterRes.Body.String())
	}
}

func TestAnalyticsEndpoint(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/ai/analytics/overview", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"totalRuns"`) {
		t.Fatalf("analytics failed: code = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestAgentsEndpoints(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/ai/agents", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "HR Assistant") {
		t.Fatalf("list agents failed: code = %d, body = %s", res.Code, res.Body.String())
	}

	saveReq := httptest.NewRequest(http.MethodPost, "/api/ai/agents", strings.NewReader(`{"name":"Custom Agent","department":"Tech","modelId":"gemini-2.5-flash","temperature":0.3,"systemPrompt":"test"}`))
	saveRes := httptest.NewRecorder()
	router.ServeHTTP(saveRes, saveReq)
	if saveRes.Code != http.StatusOK || !strings.Contains(saveRes.Body.String(), "Custom Agent") {
		t.Fatalf("save agent failed: code = %d, body = %s", saveRes.Code, saveRes.Body.String())
	}
}
