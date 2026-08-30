package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// With persistence, the saved tenant configuration is the only model source:
// a missing row must fail the run with guidance, not silently fall back to
// the platform env key.
func TestAgentLoopRequiresSavedTenantSettings(t *testing.T) {
	store := &fakeSettingsStore{} // TenantSettingsStore with no saved row
	resolver := tools.NewRegistry(handlerTestTool{})
	options := RouterOptions{
		ModelProvider: model.NewClient("http://env-key-fallback.invalid", "env-key", "env-model", nil),
	}
	router := NewRouterWithOptions(store, resolver, options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 SSE, got %d", res.Code)
	}
	events := decodeSSEEvents(t, res.Body.String())
	foundError, foundFinish := false, false
	for _, event := range events {
		switch event["type"] {
		case "error":
			if event["errorText"] == "ai.model_unavailable" {
				foundError = true
			}
		case "finish":
			foundFinish = true
		}
	}
	if !foundFinish || !foundError {
		t.Fatalf("expected error part with ai.model_unavailable followed by finish; events: %v", events)
	}
	if raw, err := json.Marshal(store.finished); err != nil || string(raw) != "true" {
		t.Fatalf("run must be persisted as FAILED: %v %s", err, raw)
	}
}

// Spike mode (store without TenantSettingsStore) keeps using the env provider.
func TestAgentLoopSpikeModeUsesEnvProvider(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"content":"Xin chào!"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}})
	defer server.Close()

	store := &agentRunStore{} // no TenantSettingsStore
	resolver := tools.NewRegistry(handlerTestTool{})
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	router := NewRouterWithOptions(store, resolver, options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	events := decodeSSEEvents(t, res.Body.String())
	text := ""
	for _, event := range events {
		if event["type"] == "text-delta" {
			text += event["delta"].(string)
		}
	}
	if text != "Xin chào!" {
		t.Fatalf("spike mode should stream via env provider, got %q (events %v)", text, eventTypes(events))
	}
}
