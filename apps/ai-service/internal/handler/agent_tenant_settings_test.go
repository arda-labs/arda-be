package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// With persistence, an active tenant setting is the model source of truth;
// the platform provider fallback only serves non-persistent development
// stores. Production never wires an env provider (cmd/ai-service/main.go),
// so a missing tenant row fails the run with the "configure in AI Settings"
// guidance instead of silently using deployment config.
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

func TestAgentLoopOpenCodeGoUsesOpaqueStableThreadSession(t *testing.T) {
	var sessions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessions = append(sessions, r.Header.Get("x-opencode-session"))
		if got := r.Header.Get("User-Agent"); got != "arda-ai-service/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	store := &fakeSettingsStore{settings: map[string]*repository.TenantSettings{
		"tenant-1": {TenantID: "tenant-1", ProviderType: "opencode-go", BaseURL: server.URL, APIKey: "key", ModelID: "glm-5.3"},
	}}
	router := NewRouterWithOptions(store, tools.NewRegistry(handlerTestTool{}), RouterOptions{ModelSessionSecret: "test-secret"})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"thread-1","runId":"run-1","messages":[{"role":"user","content":"hello"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK || len(sessions) != 1 || sessions[0] == "" {
		t.Fatalf("expected OpenCode request with session, code=%d sessions=%q", res.Code, sessions)
	}
	if sessions[0] != model.StableSessionID("test-secret", "tenant-1", "thread-1") {
		t.Fatalf("unexpected session: %q", sessions[0])
	}
}
