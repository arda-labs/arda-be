package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

func TestSSEWriterEmitsVersionedSequenceAndSingleTerminalEvent(t *testing.T) {
	response := httptest.NewRecorder()
	writer, ok := newSSEWriter(response)
	if !ok {
		t.Fatal("response recorder must support SSE flushing")
	}
	writer.event(agentEvent{Type: "RUN_STARTED", ThreadID: "thread-1", RunID: "run-1"})
	writer.event(agentEvent{Type: "TEXT_MESSAGE_CONTENT", MessageID: "message-1", Delta: "hello"})
	writer.event(agentEvent{Type: "RUN_FINISHED", ThreadID: "thread-1", RunID: "run-1"})
	// A stale callback after terminal completion must be ignored.
	writer.event(agentEvent{Type: "TEXT_MESSAGE_CONTENT", MessageID: "message-1", Delta: "stale"})

	if got := response.Header().Get(agUIProtocolVersionHeader); got != agUIProtocolVersion {
		t.Fatalf("protocol header = %q, want %q", got, agUIProtocolVersion)
	}
	events := decodeSSEEvents(t, response.Body.String())
	wantTypes := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if got := eventTypes(events); len(got) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d (%v)", len(got), len(wantTypes), got)
	}
	for i, want := range wantTypes {
		if events[i]["type"] != want {
			t.Fatalf("event %d type = %v, want %s", i, events[i]["type"], want)
		}
		if events[i]["protocolVersion"] != agUIProtocolVersion {
			t.Fatalf("event %d protocol version = %v", i, events[i]["protocolVersion"])
		}
		if sequence, ok := events[i]["sequence"].(float64); !ok || int(sequence) != i+1 {
			t.Fatalf("event %d sequence = %v, want %d", i, events[i]["sequence"], i+1)
		}
		if eventID, ok := events[i]["eventId"].(string); !ok || !strings.HasPrefix(eventID, "run-1:") {
			t.Fatalf("event %d id = %v", i, events[i]["eventId"])
		}
	}
	if _, ok := events[len(events)-1]["outcome"].(map[string]any); !ok {
		t.Fatalf("terminal success outcome missing: %v", events[len(events)-1])
	}
}

func TestSSEWriterRunErrorIsTerminalAndCarriesStableCode(t *testing.T) {
	response := httptest.NewRecorder()
	writer, _ := newSSEWriter(response)
	writer.event(agentEvent{Type: "RUN_STARTED", ThreadID: "thread-1", RunID: "run-1"})
	writer.event(agentEvent{Type: "RUN_FINISHED", ThreadID: "thread-1", RunID: "run-1", Error: "ai.model_unavailable"})
	writer.event(agentEvent{Type: "RUN_FINISHED", ThreadID: "thread-1", RunID: "run-1"})

	events := decodeSSEEvents(t, response.Body.String())
	if got := eventTypes(events); len(got) != 2 || got[1] != "RUN_ERROR" {
		t.Fatalf("terminal error sequence = %v", got)
	}
	if events[1]["message"] != "ai.model_unavailable" || events[1]["code"] != "ai.model_unavailable" {
		t.Fatalf("error contract = %v", events[1])
	}
	if events[1]["threadId"] != "thread-1" || events[1]["runId"] != "run-1" {
		t.Fatalf("error correlation = %v", events[1])
	}
}

type cancellationProvider struct {
	cancel context.CancelFunc
}

func (c cancellationProvider) StreamChat(ctx context.Context, _ []model.Message, _ []model.ToolDef, _ model.StreamCallbacks) (string, model.Usage, error) {
	if c.cancel != nil {
		c.cancel()
	}
	<-ctx.Done()
	return "", model.Usage{}, ctx.Err()
}

type cancellationRunStore struct {
	statuses []string
}

func (s *cancellationRunStore) Start(context.Context, repository.RunContext, string) error {
	return nil
}

func (s *cancellationRunStore) Finish(_ context.Context, _ repository.RunContext, _ string, status string) error {
	s.statuses = append(s.statuses, status)
	return nil
}

func TestAgentCancellationEmitsTerminalErrorAndPersistsCancelled(t *testing.T) {
	store := &cancellationRunStore{}
	request := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"thread-1","runId":"run-cancel","messages":[{"role":"user","content":"hello"}]}`))
	baseContext, cancel := context.WithCancel(request.Context())
	request = request.WithContext(baseContext)
	request.Header.Set("X-Auth-Checked", "true")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Tenant-Id", "tenant-1")
	request.Header.Set("X-Permissions", "ai.assistant.use")
	router := NewRouterWithOptions(store, nil, RouterOptions{ModelProvider: cancellationProvider{cancel: cancel}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	events := decodeSSEEvents(t, response.Body.String())
	if len(events) != 2 || events[1]["type"] != "RUN_ERROR" {
		t.Fatalf("cancellation events = %v", eventTypes(events))
	}
	if events[1]["code"] != "ai.run_cancelled" {
		t.Fatalf("cancellation code = %v", events[1]["code"])
	}
	if len(store.statuses) != 1 || store.statuses[0] != "CANCELLED" {
		t.Fatalf("persisted statuses = %v", store.statuses)
	}
}

func TestAgentRejectsUnsupportedProtocolVersionBeforeOpeningStream(t *testing.T) {
	router := NewRouter()
	request := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"protocolVersion":"ag-ui-v0","threadId":"thread-1","runId":"run-1","messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("X-Auth-Checked", "true")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Tenant-Id", "tenant-1")
	request.Header.Set("X-Permissions", "ai.assistant.use")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "ai.protocol_version_unsupported") {
		t.Fatalf("unsupported version response = %d/%s", response.Code, response.Body.String())
	}
}
