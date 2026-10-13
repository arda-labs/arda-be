package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// finishCapturingStore records what the handler persists as the tool result,
// so tests can lock the stored copy independently of the SSE copy.
type finishCapturingStore struct {
	agentRunStore
	finishedContent string
	finishedStatus  string
}

func (s *finishCapturingStore) FinishTool(_ context.Context, _, status, content, _ string) error {
	s.toolFinished = true
	s.finishedStatus = status
	s.finishedContent = content
	return nil
}

// newCapturingModelServer is newModelServer plus a record of every request body,
// so tests can inspect exactly what the model was handed back after a tool call.
func newCapturingModelServer(t *testing.T, turns [][]string, bodies *[]string) *httptest.Server {
	t.Helper()
	var index int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(raw))
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

// toolCallTurns builds the model stream for a single tool call with the given
// arguments, followed by a final text turn.
func toolCallTurns(t *testing.T, toolName string, arguments any) [][]string {
	t.Helper()
	// A model emits literal < > & in tool arguments; do not let the test
	// harness HTML-escape them, or size limits would be measured on a bigger
	// payload than the tool actually receives.
	argsJSON, err := marshalNoHTMLEscape(arguments)
	if err != nil {
		t.Fatal(err)
	}
	argsString, err := json.Marshal(string(argsJSON))
	if err != nil {
		t.Fatal(err)
	}
	return [][]string{
		{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"` + toolName + `","arguments":` + string(argsString) + `}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		},
		{
			`{"choices":[{"delta":{"content":"done"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		},
	}
}

func marshalNoHTMLEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// runAgent drives one agent request and returns the raw SSE body.
func runAgent(t *testing.T, store runStore, resolver toolResolver, serverURL string, client *http.Client) string {
	t.Helper()
	options := RouterOptions{ModelProvider: model.NewClient(serverURL, "k", "m", client), AgentMaxSteps: 3}
	router := NewRouterWithOptions(store, resolver, options)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"x"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	return res.Body.String()
}

// toolResultEventContent returns the Content of the (single) TOOL_CALL_RESULT
// event that carries a string content, i.e. what the browser renders from.
func toolResultEventContent(t *testing.T, body string) string {
	t.Helper()
	var found []string
	for _, event := range decodeSSEEvents(t, body) {
		if event["type"] != "TOOL_CALL_RESULT" {
			continue
		}
		if content, ok := event["content"].(string); ok {
			found = append(found, content)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one TOOL_CALL_RESULT with content, got %d", len(found))
	}
	return found[0]
}

// lastToolMessageContent extracts the content of the tool-role message from the
// follow-up model request: what the MODEL sees after the tool ran.
func lastToolMessageContent(t *testing.T, bodies []string) string {
	t.Helper()
	if len(bodies) < 2 {
		t.Fatalf("expected a follow-up model request, got %d request(s)", len(bodies))
	}
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(bodies[1]), &request); err != nil {
		t.Fatalf("bad follow-up model request: %v", err)
	}
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role != "tool" {
			continue
		}
		var text string
		if err := json.Unmarshal(request.Messages[i].Content, &text); err != nil {
			t.Fatalf("tool message content is not a string: %s", request.Messages[i].Content)
		}
		return text
	}
	t.Fatalf("no tool message in follow-up model request: %s", bodies[1])
	return ""
}

// TestAgentLoopRendersWidgetResultUnbounded locks the renderWidget bypass of the
// 8 KB model-content bound: the browser and the stored row get the full,
// valid-JSON widget payload; the model only gets a compact acknowledgement.
func TestAgentLoopRendersWidgetResultUnbounded(t *testing.T) {
	arguments := map[string]any{
		"title":          "Mô phỏng khoản vay",
		"html":           "<div id=\"app\">" + strings.Repeat("<p>row</p>", 2000) + "</div>",
		"js":             "document.getElementById('app');",
		"data":           map[string]any{"a": 1},
		"inputs_summary": "Điều chỉnh lãi suất và kỳ hạn.",
	}
	var bodies []string
	server := newCapturingModelServer(t, toolCallTurns(t, "renderWidget", arguments), &bodies)
	defer server.Close()

	store := &finishCapturingStore{}
	body := runAgent(t, store, tools.NewRegistry(tools.NewWidgetMetaTool()), server.URL, server.Client())

	persisted := store.finishedContent
	if len(persisted) <= modelResultContentLimit || !json.Valid([]byte(persisted)) {
		t.Fatalf("persisted content must be the full valid JSON payload (> %d bytes), got %d bytes valid=%v", modelResultContentLimit, len(persisted), json.Valid([]byte(persisted)))
	}
	if store.finishedStatus != "SUCCEEDED" {
		t.Fatalf("persisted status = %q, want SUCCEEDED", store.finishedStatus)
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(persisted), &stored); err != nil || stored["widget"] != true {
		t.Fatalf("persisted payload must carry widget:true, got err=%v widget=%v", err, stored["widget"])
	}

	streamed := toolResultEventContent(t, body)
	if streamed != persisted {
		t.Fatalf("SSE content (%d bytes) must equal the persisted payload (%d bytes)", len(streamed), len(persisted))
	}

	feedback := lastToolMessageContent(t, bodies)
	if len(feedback) >= 1024 || !strings.Contains(feedback, `"rendered":true`) || !strings.Contains(feedback, "Rendered widget") {
		t.Fatalf("model feedback must be a compact acknowledgement (<1 KB, rendered:true), got %d bytes: %.200s", len(feedback), feedback)
	}
	if strings.Contains(feedback, "<div") {
		t.Fatalf("model feedback must not echo the widget code: %.200s", feedback)
	}
}

// bigResultTool returns a payload far above the 8 KB model-content bound.
type bigResultTool struct{ summary string }

func (bigResultTool) Definition() tools.Definition {
	return tools.Definition{Name: "test.big", Version: 1, Kind: "read", Timeout: time.Second, Risk: "low"}
}

func bigResultData() json.RawMessage {
	data, _ := json.Marshal(map[string]any{"rows": strings.Repeat("x", 3*modelResultContentLimit)})
	return data
}

func (b bigResultTool) Execute(_ context.Context, _ tools.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{Data: bigResultData(), Summary: b.summary}, nil
}

// TestAgentLoopStillBoundsOtherToolResults is the contrast for the widget
// bypass: every other tool keeps the old behaviour, byte for byte.
func TestAgentLoopStillBoundsOtherToolResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary string
	}{{"without summary", ""}, {"with summary", "Big result."}} {
		t.Run(tc.name, func(t *testing.T) {
			var bodies []string
			server := newCapturingModelServer(t, toolCallTurns(t, "test.big", map[string]any{}), &bodies)
			defer server.Close()

			store := &finishCapturingStore{}
			body := runAgent(t, store, tools.NewRegistry(bigResultTool{summary: tc.summary}), server.URL, server.Client())

			wantPersisted := boundContent(string(bigResultData()))
			if store.finishedContent != wantPersisted || len(store.finishedContent) > modelResultContentLimit {
				t.Fatalf("persisted content must stay bounded: got %d bytes, want %d", len(store.finishedContent), len(wantPersisted))
			}

			streamed := toolResultEventContent(t, body)
			feedback := lastToolMessageContent(t, bodies)
			if streamed != feedback {
				t.Fatalf("SSE content must equal the model feedback for non-widget tools: %d vs %d bytes", len(streamed), len(feedback))
			}
			if len(streamed) > modelResultContentLimit {
				t.Fatalf("SSE content must stay bounded, got %d bytes", len(streamed))
			}
			if tc.summary == "" && streamed != wantPersisted {
				t.Fatalf("without a summary the SSE/feedback content is the persisted content")
			}
			if tc.summary != "" && streamed != compactToolFeedback(bigResultData(), tc.summary) {
				t.Fatalf("with a summary the SSE/feedback content is compactToolFeedback output")
			}
		})
	}
}

// widgetEventSizes runs a renderWidget call through the agent loop and returns
// the size, in UTF-16 units of the SSE data line (what the FE transport counts
// against maxEventBytes), of the TOOL_CALL_RESULT and TOOL_CALL_ARGS events,
// plus the persisted payload length.
func widgetEventSizes(t *testing.T, htmlPart, cssPart, jsPart string, data []byte) (resultUnits, argsUnits, persisted int) {
	t.Helper()
	arguments := map[string]any{
		"title": "w", "html": htmlPart, "css": cssPart, "js": jsPart,
		"data": json.RawMessage(data), "inputs_summary": "s",
	}
	var bodies []string
	server := newCapturingModelServer(t, toolCallTurns(t, "renderWidget", arguments), &bodies)
	defer server.Close()
	store := &finishCapturingStore{}
	body := runAgent(t, store, tools.NewRegistry(tools.NewWidgetMetaTool()), server.URL, server.Client())

	if store.finishedStatus != "SUCCEEDED" || !json.Valid([]byte(store.finishedContent)) {
		t.Fatalf("limit-sized widget must execute and persist valid JSON: status=%q valid=%v", store.finishedStatus, json.Valid([]byte(store.finishedContent)))
	}
	utf16Units := func(s string) int {
		n := 0
		for _, r := range s {
			if r >= 0x10000 {
				n += 2
			} else {
				n++
			}
		}
		return n
	}
	for _, l := range strings.Split(body, "\n") {
		if !strings.HasPrefix(l, "data: ") {
			continue
		}
		line := strings.TrimPrefix(l, "data: ")
		switch {
		case strings.Contains(line, `"TOOL_CALL_RESULT"`) && strings.Contains(line, `"content"`):
			resultUnits = utf16Units(line)
		case strings.Contains(line, `"TOOL_CALL_ARGS"`):
			argsUnits = utf16Units(line)
		}
	}
	if resultUnits == 0 {
		t.Fatal("no TOOL_CALL_RESULT content event found")
	}
	return resultUnits, argsUnits, len(store.finishedContent)
}

const browserEventLimit = 256 * 1024 // FE transport maxEventBytes (packages/api/src/ai-agent.ts)

// TestAgentLoopWidgetAtLimitsFitsBrowserEventLimit: a widget at both size
// limits with realistic content (typical markup/JS with ordinary quote and
// newline density, tabular JSON data) must stream as one event the browser
// accepts.
func TestAgentLoopWidgetAtLimitsFitsBrowserEventLimit(t *testing.T) {
	const codeLimit, dataLimit = 48 * 1024, 64 * 1024
	repeatTo := func(unit string, limit int) string {
		return strings.Repeat(unit, limit/len(unit))
	}
	htmlPart := repeatTo("<div class=\"row\"><span>Lãi suất</span><input id=\"rate\" value=\"12.5\"></div>\n", codeLimit/3)
	cssPart := repeatTo(".row { display: flex; color: var(--foreground); }\n", codeLimit/3)
	jsPart := repeatTo("const el = document.getElementById('rate'); el.addEventListener('input', () => render(el.value));\n", codeLimit-len(htmlPart)-len(cssPart))
	data := "[" + repeatTo(`{"month":1,"balance":1234567.89,"label":"Tháng 1"},`, dataLimit-64) + `{"month":0}]`
	if len(htmlPart)+len(cssPart)+len(jsPart) > codeLimit || len(data) > dataLimit {
		t.Fatalf("test input exceeds widget limits: code=%d data=%d", len(htmlPart)+len(cssPart)+len(jsPart), len(data))
	}

	resultUnits, argsUnits, persisted := widgetEventSizes(t, htmlPart, cssPart, jsPart, []byte(data))
	t.Logf("realistic at-limit widget: TOOL_CALL_RESULT = %d, TOOL_CALL_ARGS = %d UTF-16 units (limit %d), persisted = %d bytes", resultUnits, argsUnits, browserEventLimit, persisted)
	if resultUnits > browserEventLimit {
		t.Fatalf("TOOL_CALL_RESULT event is %d UTF-16 units, over the browser limit %d", resultUnits, browserEventLimit)
	}
}

// TestAgentLoopWidgetAdversarialEscapingMeasurement records how far a
// maliciously escape-heavy (but still valid) widget overshoots the browser
// event limit. It does not fail on the overshoot: the numbers are reported as
// a known concern (see the Task 7 report), not asserted away.
func TestAgentLoopWidgetAdversarialEscapingMeasurement(t *testing.T) {
	const codeLimit, dataLimit = 48 * 1024, 64 * 1024
	for _, tc := range []struct{ name, fill string }{
		{"quotes and newlines", "\"\n"},
		{"html angle brackets", "<"},
		{"non-ASCII", "ệ"},
		{"backslashes", "\\"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := strings.Repeat(tc.fill, codeLimit/len(tc.fill))
			third := len(code) / 3
			for third > 0 && !utf8.ValidString(code[:third]) {
				third--
			}
			htmlPart := code[:third]
			cssPart := strings.Repeat(tc.fill, third/len(tc.fill))
			jsPart := strings.Repeat(tc.fill, (codeLimit-len(htmlPart)-len(cssPart))/len(tc.fill))
			unit, _ := marshalNoHTMLEscape(tc.fill)
			encoded, _ := marshalNoHTMLEscape(strings.Repeat(tc.fill, (dataLimit-2)/(len(unit)-2)))

			resultUnits, argsUnits, persisted := widgetEventSizes(t, htmlPart, cssPart, jsPart, encoded)
			t.Logf("%s: TOOL_CALL_RESULT = %d, TOOL_CALL_ARGS = %d UTF-16 units (limit %d), persisted = %d bytes", tc.name, resultUnits, argsUnits, browserEventLimit, persisted)
		})
	}
}
