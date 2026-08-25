package handler

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
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

func TestRunStreamsDeterministicAGUIEvents(t *testing.T) {
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
