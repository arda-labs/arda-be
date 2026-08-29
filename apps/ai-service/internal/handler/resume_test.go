package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type resumeTestStore struct {
	executionTestStore
	resumed     bool
	runMessages []repository.HistoryMessage
}

func (s *resumeTestStore) RunMessages(_ context.Context, _ repository.RunContext) ([]repository.HistoryMessage, error) {
	return s.runMessages, nil
}

func (s *resumeTestStore) ResumeRun(_ context.Context, _ repository.RunContext) error {
	s.resumed = true
	return nil
}

// After an approved tool executes, the agent loop must resume: the response
// streams the next model turn and the run is finished by the loop, not inline.
func TestExecuteApprovedToolResumesAgentLoop(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"content":"Đã xuất xong dữ liệu."}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}})
	defer server.Close()

	store := &resumeTestStore{
		runMessages: []repository.HistoryMessage{
			{Role: "user", Content: "xuất khách hàng"},
		},
	}
	resolver := tools.NewRegistry(handlerConfirmTool{})
	router := NewRouterWithOptions(store, resolver, RouterOptions{
		EnableHITLProposals: true,
		ModelProvider:       model.NewClient(server.URL, "k", "m", server.Client()),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/a1/execution", nil)
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !store.resumed {
		t.Fatalf("ResumeRun was not called")
	}
	if !store.finishToolCalled {
		t.Fatalf("tool execution not persisted as SUCCEEDED")
	}

	events := decodeSSEEvents(t, res.Body.String())
	text := ""
	finished := false
	for _, event := range events {
		switch event["type"] {
		case "TEXT_MESSAGE_CONTENT":
			text += event["delta"].(string)
		case "RUN_FINISHED":
			finished = true
		}
	}
	if text != "Đã xuất xong dữ liệu." {
		t.Fatalf("expected resumed assistant reply in stream, got %q", text)
	}
	if !finished || !store.finished {
		t.Fatalf("resumed run did not finish: streamFinished=%v storeFinished=%v", finished, store.finished)
	}
}

// The resumed provider conversation must pair the executed tool call with its
// tool result, or strict providers reject the request.
func TestBuildResumeMessagesPairsToolCall(t *testing.T) {
	store := &resumeTestStore{
		runMessages: []repository.HistoryMessage{
			{Role: "user", Content: "xuất khách hàng"},
		},
	}
	options := RouterOptions{ModelSystemPrompt: "Bạn là Olorin."}
	exec := repository.ApprovedExecution{
		ExecutionID: "exec-1",
		Run: repository.RunContext{
			TenantID: "tenant-1", ActorUserID: "user-1",
			ExternalThread: "t1", ExternalRun: "r1",
		},
		ToolName: "test.confirm", ToolVersion: 1, Arguments: `{"format":"csv"}`,
	}

	messages := buildResumeMessages(context.Background(), store, options, exec, `{"prepared":true}`)

	if len(messages) != 4 {
		t.Fatalf("expected [system user assistant tool], got %d messages: %+v", len(messages), messages)
	}
	if messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("unexpected leading roles: %+v", messages)
	}
	assistant := messages[2]
	tool := messages[3]
	if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected assistant tool_calls message, got %+v", assistant)
	}
	if tool.Role != "tool" || tool.ToolCallID != assistant.ToolCalls[0].ID {
		t.Fatalf("tool message not paired with tool_call id: %+v vs %+v", tool, assistant)
	}
	if assistant.ToolCalls[0].Name != "test.confirm" || !json.Valid([]byte(assistant.ToolCalls[0].Arguments)) {
		t.Fatalf("tool call fields wrong: %+v", assistant.ToolCalls[0])
	}
}
