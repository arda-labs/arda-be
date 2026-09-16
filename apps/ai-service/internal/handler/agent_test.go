package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type agentRunStore struct {
	fakeToolRunStore
	usageSet bool
}

func (s *agentRunStore) SetUsage(_ context.Context, _ repository.RunContext, _ string) error {
	s.usageSet = true
	return nil
}

func newModelServer(t *testing.T, turns [][]string) *httptest.Server {
	t.Helper()
	var index int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if index >= len(turns) {
			t.Fatalf("unexpected extra model request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range turns[index] {
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		index++
	}))
}

func gatewayHeaders(req *http.Request) {
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-Permissions", "superadmin")
}

func decodeSSEEvents(t *testing.T, body string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("bad SSE payload: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func eventTypes(events []map[string]any) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event["type"].(string))
	}
	return types
}

func TestAgentLoopStreamsTextAndExecutesReadTool(t *testing.T) {
	server := newModelServer(t, [][]string{
		{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"test.read","arguments":"{\"customerId\":\"c1\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		},
		{
			`{"choices":[{"delta":{"content":"Khách hàng đang hoạt động."}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		},
	})
	defer server.Close()

	store := &agentRunStore{}
	resolver := tools.NewRegistry(handlerTestTool{})
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client()), AgentMaxSteps: 3}
	router := NewRouterWithOptions(store, resolver, options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xem khách hàng"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	events := decodeSSEEvents(t, res.Body.String())
	types := eventTypes(events)
	for _, expected := range []string{"RUN_STARTED", "TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "TOOL_CALL_RESULT", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"} {
		found := false
		for _, actual := range types {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s in events: %v", expected, types)
		}
	}
	if !store.toolStarted || !store.toolFinished || !store.finished {
		t.Fatalf("tool persistence or run finish missing: %+v", store)
	}
}

func TestAgentLoopCreatesApprovalProposalForConfirmTool(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-9","type":"function","function":{"name":"test.confirm","arguments":"{\"format\":\"csv\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}})
	defer server.Close()

	store := &agentRunStore{}
	resolver := tools.NewRegistry(handlerConfirmTool{})
	options := RouterOptions{
		ModelProvider:       model.NewClient(server.URL, "k", "m", server.Client()),
		AgentMaxSteps:       3,
		EnableHITLProposals: true,
	}
	router := NewRouterWithOptions(store, resolver, options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xuất dữ liệu"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	events := decodeSSEEvents(t, res.Body.String())
	var proposal map[string]any
	for _, event := range events {
		if event["type"] == "TOOL_CALL_RESULT" {
			content, ok := event["content"].(string)
			if !ok {
				t.Fatalf("tool result content is not a string: %v", event["content"])
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(content), &payload); err != nil {
				t.Fatalf("tool result content not JSON: %s", content)
			}
			raw, _ := json.Marshal(payload["proposal"])
			if string(raw) == "null" && payload["denied"] != nil {
				continue
			}
			if err := json.Unmarshal(raw, &proposal); err != nil || proposal["id"] == nil {
				t.Fatalf("expected proposal in tool result, got %s", content)
			}
		}
	}
	if proposal == nil {
		t.Fatalf("no approval proposal emitted; events: %v", eventTypes(events))
	}
	if !store.approvalCreated {
		t.Fatalf("approval proposal was not persisted")
	}
	if store.finished {
		t.Fatalf("run must stay WAITING_APPROVAL, not finished")
	}
}

// TestAgentLoopCodeModeApprovalEmitsInterrupt locks the Code Mode HITL path:
// the sandbox meta-tool returns a typed Result.Approval, the handler emits the
// standard proposal payload, RUN_FINISHED carries outcome=interrupt, and the
// run is not finished.
func TestAgentLoopCodeModeApprovalEmitsInterrupt(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-9","type":"function","function":{"name":"execute","arguments":"{\"code\":\"return await arda.crm.exportCustomer({ customerId: 'c1' })\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}})
	defer server.Close()

	store := &agentRunStore{}
	resolver := tools.NewRegistry(handlerApprovalPendingTool{})
	options := RouterOptions{
		ModelProvider:       model.NewClient(server.URL, "k", "m", server.Client()),
		AgentMaxSteps:       3,
		EnableHITLProposals: true,
	}
	router := NewRouterWithOptions(store, resolver, options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xuất dữ liệu"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	events := decodeSSEEvents(t, res.Body.String())
	var proposalID any
	var finishedOutcome map[string]any
	for _, event := range events {
		switch event["type"] {
		case "TOOL_CALL_RESULT":
			content, _ := event["content"].(string)
			var payload map[string]any
			if json.Unmarshal([]byte(content), &payload) == nil {
				if proposal, ok := payload["proposal"].(map[string]any); ok {
					proposalID = proposal["id"]
				}
			}
		case "RUN_FINISHED":
			if outcome, ok := event["outcome"].(map[string]any); ok {
				finishedOutcome = outcome
			}
		}
	}
	if proposalID != "approval-9" {
		t.Fatalf("proposal id = %v, want approval-9; events: %v", proposalID, eventTypes(events))
	}
	if finishedOutcome == nil || finishedOutcome["type"] != "interrupt" {
		t.Fatalf("RUN_FINISHED outcome = %v, want interrupt", finishedOutcome)
	}
	if store.finished {
		t.Fatal("run must stay WAITING_APPROVAL, not finished")
	}
	if !store.toolFinished {
		t.Fatal("the meta-tool execution row must be finished, not left dangling")
	}
}

type handlerApprovalPendingTool struct{}

func (handlerApprovalPendingTool) Definition() tools.Definition {
	return tools.Definition{Name: "execute", Version: 1, Kind: "read", Timeout: time.Second, Risk: "low"}
}

func (handlerApprovalPendingTool) Execute(_ context.Context, _ tools.Context, _ json.RawMessage) (tools.Result, error) {
	data, _ := json.Marshal(map[string]any{"status": "WAITING_APPROVAL"})
	return tools.Result{
		Data:     data,
		Summary:  "waiting for approval",
		Source:   "ai-sandbox",
		Approval: &tools.ApprovalPending{ProposalID: "approval-9", Tool: "crm.exportCustomer", Version: 1, Risk: "medium", ExpiresAt: time.Now().UTC().Add(15 * time.Minute)},
	}, nil
}

func TestExecuteApprovedToolRequiresOwnerAndApprovalState(t *testing.T) {
	resolver := tools.NewRegistry(handlerConfirmTool{})
	router := NewRouterWithOptions(&executionTestStore{notFound: true}, resolver, RouterOptions{EnableHITLProposals: true})

	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/a1/execution", nil)
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no approved execution exists, got %d", res.Code)
	}
}

func TestExecuteApprovedToolRunsToolAndFinishesRun(t *testing.T) {
	store := &executionTestStore{}
	resolver := tools.NewRegistry(handlerConfirmTool{})
	router := NewRouterWithOptions(store, resolver, RouterOptions{EnableHITLProposals: true})

	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/a1/execution", nil)
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var response struct {
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("bad response body: %v", err)
	}
	if response.Result.Status != "EXECUTED" {
		t.Fatalf("unexpected status: %q", response.Result.Status)
	}
	if !store.finishToolCalled || !store.finished {
		t.Fatalf("persistence not updated: finishTool=%v finished=%v", store.finishToolCalled, store.finished)
	}
}

type handlerConfirmTool struct{}

func (handlerConfirmTool) Definition() tools.Definition {
	return tools.Definition{Name: "test.confirm", Version: 1, Kind: "confirm", RequiredPermissions: []string{"crm.customer.read"}, Risk: "confirm"}
}

func (handlerConfirmTool) Execute(_ context.Context, _ tools.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{Data: json.RawMessage(`{"prepared":true}`), Summary: "Đã chuẩn bị."}, nil
}

type executionTestStore struct {
	fakeRunStore
	started           bool
	notFound          bool
	finishToolCalled  bool
	arguments         string
	permissionVersion string
	resourceVersion   string
	lastToolStatus    string
	lastToolErrorCode string
}

func (s *executionTestStore) Start(_ context.Context, run repository.RunContext, _ string) error {
	s.started = true
	s.fakeRunStore.started = run
	return nil
}

func (s *executionTestStore) FetchApprovedExecution(_ context.Context, _, _, _ string) (repository.ApprovedExecution, error) {
	if s.notFound {
		return repository.ApprovedExecution{}, repository.ErrApprovalNotFound
	}
	if !s.started {
		s.started = true
		s.fakeRunStore.started = repository.RunContext{TenantID: "tenant-1", ActorUserID: "user-1", ExternalThread: "t1", ExternalRun: "r1"}
	}
	arguments := s.arguments
	if arguments == "" {
		arguments = `{"format":"csv"}`
	}
	return repository.ApprovedExecution{
		ExecutionID:       "exec-1",
		Run:               s.fakeRunStore.started,
		ToolName:          "test.confirm",
		ToolVersion:       1,
		Arguments:         arguments,
		PermissionVersion: s.permissionVersion,
		ResourceVersion:   s.resourceVersion,
	}, nil
}

func (s *executionTestStore) StartTool(context.Context, repository.RunContext, string, int, string, string, string) (string, error) {
	return "exec-1", nil
}

func (s *executionTestStore) FinishTool(_ context.Context, _, status, _, errorCode string) error {
	s.finishToolCalled = status == "SUCCEEDED"
	s.lastToolStatus = status
	s.lastToolErrorCode = errorCode
	return nil
}

// scopeCapturingTool records the tools.Context it was executed with, so tests
// can assert what the handler put on the run scratch space.
type scopeCapturingTool struct {
	scope tools.Context
}

func (t *scopeCapturingTool) Definition() tools.Definition {
	return tools.Definition{
		Name:                "test.read",
		Version:             1,
		Kind:                "read",
		RequiredPermissions: []string{"crm.customer.read"},
		Risk:                "low",
	}
}

func (t *scopeCapturingTool) Execute(_ context.Context, scope tools.Context, _ json.RawMessage) (tools.Result, error) {
	t.scope = scope
	return tools.Result{Data: json.RawMessage(`{}`), Summary: "ok"}, nil
}

// TestAgentToolExecutionCarriesRunIdentity locks the handler → tool plumbing
// Code Mode HITL depends on: a model-initiated tool call must carry the AG-UI
// run ids on its scope so the sandbox can attach a proposal to the owning
// ai_runs row.
func TestAgentToolExecutionCarriesRunIdentity(t *testing.T) {
	server := newModelServer(t, [][]string{
		{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"test.read","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		},
		{
			`{"choices":[{"delta":{"content":"done"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		},
	})
	defer server.Close()

	capture := &scopeCapturingTool{}
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client()), AgentMaxSteps: 3}
	router := NewRouterWithOptions(&agentRunStore{}, tools.NewRegistry(capture), options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"x"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if capture.scope.ExternalThread != "t1" || capture.scope.ExternalRun != "r1" {
		t.Fatalf("tool scope = thread %q/run %q, want t1/r1", capture.scope.ExternalThread, capture.scope.ExternalRun)
	}
	if capture.scope.TenantID != "tenant-1" || capture.scope.ActorUserID != "user-1" {
		t.Fatalf("tool scope ownership = %q/%q, want tenant-1/user-1", capture.scope.TenantID, capture.scope.ActorUserID)
	}
}

// TestDirectToolExecutionCarriesRunIdentity covers the explicit `tool` call
// path (POST /api/ai/agent with a run body tool), which executes the resolved
// tool without the model loop.
func TestDirectToolExecutionCarriesRunIdentity(t *testing.T) {
	capture := &scopeCapturingTool{}
	router := NewRouterWithOptions(&fakeToolRunStore{}, tools.NewRegistry(capture), RouterOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"x"}],"tool":{"name":"test.read","arguments":{}}}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if capture.scope.ExternalThread != "t1" || capture.scope.ExternalRun != "r1" {
		t.Fatalf("tool scope = thread %q/run %q, want t1/r1", capture.scope.ExternalThread, capture.scope.ExternalRun)
	}
}

// failingToolStartStore fails the audit-row write so tests can lock the
// audit-first contract: no recorded invocation, no execution.
type failingToolStartStore struct {
	fakeToolRunStore
}

func (s *failingToolStartStore) StartTool(context.Context, repository.RunContext, string, int, string, string, string) (string, error) {
	return "", errors.New("audit store unavailable")
}

// TestAgentToolExecutionFailsClosedWhenAuditUnavailable locks the ordering fix:
// StartTool runs BEFORE the tool, and a failure refuses the execution instead
// of running an unaudited side effect.
func TestAgentToolExecutionFailsClosedWhenAuditUnavailable(t *testing.T) {
	server := newModelServer(t, [][]string{
		{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"test.read","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		},
		{
			`{"choices":[{"delta":{"content":"done"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		},
	})
	defer server.Close()

	capture := &scopeCapturingTool{}
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client()), AgentMaxSteps: 3}
	router := NewRouterWithOptions(&failingToolStartStore{}, tools.NewRegistry(capture), options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"x"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if capture.scope.TenantID != "" {
		t.Fatalf("tool must not execute when its audit row cannot be written: %+v", capture.scope)
	}
	if !strings.Contains(res.Body.String(), "ai.tool_persistence_unavailable") {
		t.Fatalf("expected ai.tool_persistence_unavailable in stream, got %s", res.Body.String())
	}
}
