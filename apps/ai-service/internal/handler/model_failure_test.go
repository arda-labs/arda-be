package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// TestAgentModelFailureNeverPersistsProviderBody locks task #3: the upstream
// error body must not reach the transcript (it is replayed into later turns)
// or the client stream; only the stable error code is exposed.
func TestAgentModelFailureNeverPersistsProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"upstream leaked Bearer sk-live-12345"}`))
	}))
	defer server.Close()

	store := &agentRunStore{}
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	router := NewRouterWithOptions(store, tools.NewRegistry(handlerTestTool{}), options)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"hello"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected streaming 200, got %d: %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "sk-live-12345") {
		t.Fatalf("client stream leaked the provider body: %s", res.Body.String())
	}
	if strings.Contains(store.finishMessage, "sk-live-12345") || strings.Contains(store.finishMessage, "Bearer") {
		t.Fatalf("transcript leaked the provider body: %s", store.finishMessage)
	}
	if !strings.Contains(store.finishMessage, "ai.model_unavailable") {
		t.Fatalf("transcript missing the stable error code: %s", store.finishMessage)
	}
	if !strings.Contains(res.Body.String(), "ai.model_unavailable") {
		t.Fatalf("stream missing the stable error code: %s", res.Body.String())
	}
}
