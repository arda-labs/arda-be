package handler

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
