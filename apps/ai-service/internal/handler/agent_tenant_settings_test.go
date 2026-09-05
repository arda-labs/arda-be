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

// With persistence, an active tenant setting overrides the platform provider;
// a missing row uses the platform fallback so first use does not require
// duplicating deployment configuration per tenant.
func TestAgentLoopUsesPlatformFallbackWhenTenantSettingsMissing(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"content":"fallback"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}})
	defer server.Close()
	store := &fakeSettingsStore{} // TenantSettingsStore with no saved row
	resolver := tools.NewRegistry(handlerTestTool{})
	options := RouterOptions{
		ModelProvider: model.NewClient(server.URL, "env-key", "env-model", server.Client()),
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
	got := ""
	for _, event := range events {
		if event["type"] == "TEXT_MESSAGE_CONTENT" {
			got += event["delta"].(string)
		}
	}
	if got != "fallback" {
		t.Fatalf("expected platform fallback response, got %q; events: %v", got, events)
	}
	if raw, err := json.Marshal(store.finished); err != nil || string(raw) != "true" {
		t.Fatalf("run must be persisted as FAILED: %v %s", err, raw)
	}
}

// Development mode (store without TenantSettingsStore) keeps using the env provider.
func TestAgentLoopDevelopmentModeUsesEnvProvider(t *testing.T) {
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
		if event["type"] == "TEXT_MESSAGE_CONTENT" {
			text += event["delta"].(string)
		}
	}
	if text != "Xin chào!" {
		t.Fatalf("development mode should stream via env provider, got %q (events %v)", text, eventTypes(events))
	}
}
